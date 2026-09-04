package ui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/ponomarev-prime/sshfleet-console/internal/session"
)

type terminalTabState uint8

const (
	terminalTabStarting terminalTabState = iota
	terminalTabRunning
	terminalTabFinishing
	terminalTabExited
	terminalTabFailed
	terminalTabClosing
)

type terminalTab struct {
	id           uint64
	targetID     string
	label        string
	state        terminalTabState
	session      *session.Embedded
	screen       string
	errorText    string
	closePrompt  bool
	interruptOK  bool
	scrollOffset int
	scrollback   []string
}

type terminalTabStartedMsg struct {
	id      uint64
	session *session.Embedded
	err     error
}

type terminalTabOutputMsg struct {
	id      uint64
	session *session.Embedded
	data    []byte
	err     error
}

type terminalTabFinishedMsg struct {
	id         uint64
	session    *session.Embedded
	lines      []string
	screen     string
	scrollback []string
	err        error
}

type terminalTabClosedMsg struct {
	id  uint64
	err error
}

func (m Model) fullTerminalDimensions() (width, height int) {
	width, height = m.width, m.height
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}
	// The session owns everything except the tab strip and footer.
	return max(1, width), max(1, height-2)
}

func (m Model) openTerminalTab(cmd *exec.Cmd, hostIndex int, label string, interruptOK bool) (tea.Model, tea.Cmd) {
	if cmd == nil {
		m.message = "Terminal tab: command is unavailable"
		return m, nil
	}
	targetID := ""
	if hostIndex >= 0 && hostIndex < len(m.hosts) {
		targetID = m.hosts[hostIndex].ID
	}
	m.nextTabID++
	tab := terminalTab{
		id:          m.nextTabID,
		targetID:    targetID,
		label:       session.CleanText(label),
		state:       terminalTabStarting,
		interruptOK: interruptOK,
	}
	m.tabs = append(m.tabs, tab)
	m.activeTab = len(m.tabs)
	m.quitTabsConfirm = false
	width, height := m.fullTerminalDimensions()
	return m, startTerminalTabCmd(tab.id, cmd, width, height, m.terminalScrollbackLines)
}

func startTerminalTabCmd(id uint64, cmd *exec.Cmd, width, height, scrollbackLines int) tea.Cmd {
	return func() tea.Msg {
		ptySession, err := session.StartEmbedded(cmd, width, height)
		if err == nil {
			ptySession.Terminal.SetScrollbackSize(scrollbackLines)
		}
		return terminalTabStartedMsg{id: id, session: ptySession, err: err}
	}
}

func readTerminalTabCmd(id uint64, ptySession *session.Embedded) tea.Cmd {
	return func() tea.Msg {
		data, err := ptySession.ReadChunk()
		return terminalTabOutputMsg{id: id, session: ptySession, data: data, err: err}
	}
}

func finishTerminalTabCmd(id uint64, ptySession *session.Embedded, screen string, scrollback []string) tea.Cmd {
	return func() tea.Msg {
		err := ptySession.Finish()
		return terminalTabFinishedMsg{
			id: id, session: ptySession, lines: ptySession.Capture.LastLines(12), screen: screen, scrollback: scrollback, err: err,
		}
	}
}

func closeTerminalTabCmd(id uint64, ptySession *session.Embedded) tea.Cmd {
	return func() tea.Msg {
		err := ptySession.Close()
		return terminalTabClosedMsg{id: id, err: err}
	}
}

func (m *Model) terminalTabIndex(id uint64) int {
	for index := range m.tabs {
		if m.tabs[index].id == id {
			return index
		}
	}
	return -1
}

func (m Model) applyTerminalTabStarted(msg terminalTabStartedMsg) (tea.Model, tea.Cmd) {
	index := m.terminalTabIndex(msg.id)
	if index < 0 {
		if msg.session != nil {
			return m, closeTerminalTabCmd(msg.id, msg.session)
		}
		return m, nil
	}
	if msg.err != nil {
		if m.quitAfterTabsClose || m.tabs[index].state == terminalTabClosing {
			m.tabs = append(m.tabs[:index], m.tabs[index+1:]...)
			m.activeTab = 0
			if m.quitAfterTabsClose && len(m.tabs) == 0 {
				m.quitAfterTabsClose = false
				return m, tea.Quit
			}
			return m, nil
		}
		m.tabs[index].state = terminalTabFailed
		m.tabs[index].errorText = session.CleanText(msg.err.Error())
		m.message = "Terminal tab failed: " + m.tabs[index].errorText
		return m, nil
	}
	if m.tabs[index].state == terminalTabClosing {
		m.tabs[index].session = msg.session
		return m, closeTerminalTabCmd(msg.id, msg.session)
	}
	m.tabs[index].session = msg.session
	m.tabs[index].state = terminalTabRunning
	m.message = "Terminal tab active · Ctrl+G Fleet · Ctrl+] close"
	return m, readTerminalTabCmd(msg.id, msg.session)
}

func (m Model) applyTerminalTabOutput(msg terminalTabOutputMsg) (tea.Model, tea.Cmd) {
	index := m.terminalTabIndex(msg.id)
	if index < 0 || m.tabs[index].session != msg.session || m.tabs[index].state != terminalTabRunning {
		return m, nil
	}
	if len(msg.data) > 0 {
		before := msg.session.Terminal.ScrollbackLen()
		_, _ = msg.session.Terminal.Write(msg.data)
		after := msg.session.Terminal.ScrollbackLen()
		if m.tabs[index].scrollOffset > 0 && after > before {
			m.tabs[index].scrollOffset = min(after, m.tabs[index].scrollOffset+after-before)
		}
	}
	if msg.err == nil {
		return m, readTerminalTabCmd(msg.id, msg.session)
	}
	m.tabs[index].state = terminalTabFinishing
	m.tabs[index].screen = msg.session.Terminal.Render()
	m.tabs[index].scrollback = terminalScrollbackSnapshot(msg.session)
	return m, finishTerminalTabCmd(msg.id, msg.session, m.tabs[index].screen, m.tabs[index].scrollback)
}

func (m Model) applyTerminalTabFinished(msg terminalTabFinishedMsg) (tea.Model, tea.Cmd) {
	index := m.terminalTabIndex(msg.id)
	if index < 0 || m.tabs[index].session != msg.session {
		return m, nil
	}
	wasActive := m.activeTab == index+1
	tab := &m.tabs[index]
	tab.session = nil
	tab.screen = msg.screen
	tab.scrollback = append([]string(nil), msg.scrollback...)
	tab.scrollOffset = min(tab.scrollOffset, len(tab.scrollback))
	tab.closePrompt = false
	if tab.targetID != "" && len(msg.lines) > 0 {
		m.sessionTail[tab.targetID] = append([]string(nil), msg.lines...)
	}
	err := msg.err
	if tab.interruptOK && interruptedExit(err) {
		err = nil
	}
	if err == nil {
		tab.state = terminalTabExited
		tab.errorText = ""
		m.message = "Terminal tab finished · final screen kept in tab"
	} else {
		tab.state = terminalTabFailed
		tab.errorText = session.CleanText(err.Error())
		m.message = "Terminal tab finished with error: " + tab.errorText
	}
	if wasActive {
		m.activeTab = 0
	}
	if m.quitAfterTabsClose {
		m.tabs = append(m.tabs[:index], m.tabs[index+1:]...)
		m.activeTab = 0
		if len(m.tabs) == 0 {
			m.quitAfterTabsClose = false
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) applyTerminalTabClosed(msg terminalTabClosedMsg) (tea.Model, tea.Cmd) {
	index := m.terminalTabIndex(msg.id)
	if index < 0 {
		return m, nil
	}
	m.tabs = append(m.tabs[:index], m.tabs[index+1:]...)
	m.activeTab = 0
	m.quitTabsConfirm = false
	if m.quitAfterTabsClose && len(m.tabs) == 0 {
		m.quitAfterTabsClose = false
		return m, tea.Quit
	}
	if msg.err != nil && !interruptedExit(msg.err) && strings.TrimSpace(msg.err.Error()) != "signal: killed" {
		m.message = "Terminal tab close: " + session.CleanText(msg.err.Error())
	} else {
		m.message = "Terminal tab closed"
	}
	return m, nil
}

func (m Model) handleTerminalTabKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	index := m.activeTab - 1
	if index < 0 || index >= len(m.tabs) {
		m.activeTab = 0
		return m, nil
	}
	tab := &m.tabs[index]
	if next, ok := m.activateDirectTab(msg); ok {
		tab.closePrompt = false
		return next, nil
	}
	switch msg.String() {
	case "ctrl+d":
		// Ctrl+D is the product-level fast close shortcut. Remove the tab and
		// return to Fleet before waiting for the child process to stop, so a TUI
		// such as dtop cannot swallow EOF and make the interface feel stuck.
		id, ptySession := tab.id, tab.session
		m.tabs = append(m.tabs[:index], m.tabs[index+1:]...)
		m.activeTab = 0
		m.quitTabsConfirm = false
		m.message = "Terminal tab closed"
		if ptySession == nil {
			return m, nil
		}
		return m, closeTerminalTabCmd(id, ptySession)
	case "ctrl+g":
		tab.closePrompt = false
		m.activeTab = 0
		m.message = "Fleet tab"
		return m, nil
	case "ctrl+n":
		tab.closePrompt = false
		return m.activateNextTab(), nil
	case "ctrl+p":
		tab.closePrompt = false
		return m.activatePreviousTab(), nil
	case "esc":
		if tab.closePrompt {
			tab.closePrompt = false
			m.message = "Terminal tab close cancelled"
			return m, nil
		}
	case "ctrl+]":
		if tab.state == terminalTabClosing {
			return m, nil
		}
		if tab.state == terminalTabRunning || tab.state == terminalTabStarting || tab.state == terminalTabFinishing {
			if !tab.closePrompt {
				tab.closePrompt = true
				m.message = "Live terminal session · Ctrl+] again to close · Esc cancel"
				return m, nil
			}
			tab.state = terminalTabClosing
			tab.closePrompt = false
			m.message = "Closing terminal tab…"
			if tab.session == nil {
				return m, nil
			}
			return m, closeTerminalTabCmd(tab.id, tab.session)
		}
		m.tabs = append(m.tabs[:index], m.tabs[index+1:]...)
		m.activeTab = 0
		m.message = "Terminal tab closed"
		return m, nil
	}
	if tab.state != terminalTabRunning || tab.session == nil || tab.closePrompt {
		return m, nil
	}
	tab.scrollOffset = 0
	key := msg.Key()
	tab.session.Terminal.SendKey(uv.KeyPressEvent(uv.Key{
		Text: key.Text, Mod: uv.KeyMod(key.Mod), Code: key.Code,
		ShiftedCode: key.ShiftedCode, BaseCode: key.BaseCode, IsRepeat: key.IsRepeat,
	}))
	return m, nil
}

const terminalWheelLines = 3

func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	index := m.activeTab - 1
	if index < 0 || index >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[index]
	available := len(tab.scrollback)
	if tab.session != nil && tab.state == terminalTabRunning {
		available = tab.session.Terminal.ScrollbackLen()
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		tab.scrollOffset = min(available, tab.scrollOffset+terminalWheelLines)
	case tea.MouseWheelDown:
		tab.scrollOffset = max(0, tab.scrollOffset-terminalWheelLines)
	}
	return m, nil
}

func terminalScrollbackSnapshot(ptySession *session.Embedded) []string {
	if ptySession == nil || ptySession.Terminal == nil {
		return nil
	}
	scrollback := ptySession.Terminal.Scrollback()
	if scrollback == nil || scrollback.Len() == 0 {
		return nil
	}
	lines := make([]string, scrollback.Len())
	for index := range lines {
		lines[index] = scrollback.Line(index).Render()
	}
	return lines
}

func (m Model) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if msg.Content == "" {
		return m, nil
	}
	if m.activeTab > 0 {
		index := m.activeTab - 1
		if index >= 0 && index < len(m.tabs) {
			tab := &m.tabs[index]
			if tab.state == terminalTabRunning && tab.session != nil && !tab.closePrompt {
				// Paste is a single semantic event, not a series of application
				// hotkeys. The nested emulator restores bracketed-paste markers
				// when the remote shell/editor requested that mode.
				tab.session.Terminal.Paste(msg.Content)
			}
		}
		return m, nil
	}
	if m.embedded != nil && !m.embeddedClosing {
		m.embedded.Terminal.Paste(msg.Content)
	}
	return m, nil
}

func directTabIndex(msg tea.KeyPressMsg) (int, bool) {
	key := msg.Key()
	if key.Code < '1' || key.Code > '9' {
		return 0, false
	}
	// Ctrl+digit needs modifyOtherKeys/Kitty keyboard support and may be
	// reserved by the outer terminal. Alt+digit is the portable fallback.
	if key.Mod != tea.ModCtrl && key.Mod != tea.ModAlt {
		return 0, false
	}
	return int(key.Code - '1'), true
}

func (m Model) activateDirectTab(msg tea.KeyPressMsg) (Model, bool) {
	index, shortcut := directTabIndex(msg)
	if !shortcut || index > len(m.tabs) {
		return m, false
	}
	m.activeTab = index
	m.quitTabsConfirm = false
	if index == 0 {
		m.message = "Fleet tab"
	} else {
		m.message = fmt.Sprintf("Terminal tab %d", index+1)
	}
	return m, true
}

func (m Model) activateNextTab() Model {
	if len(m.tabs) == 0 {
		m.activeTab = 0
		return m
	}
	m.activeTab++
	if m.activeTab > len(m.tabs) {
		m.activeTab = 0
	}
	m.quitTabsConfirm = false
	return m
}

func (m Model) activatePreviousTab() Model {
	if len(m.tabs) == 0 {
		m.activeTab = 0
		return m
	}
	m.activeTab--
	if m.activeTab < 0 {
		m.activeTab = len(m.tabs)
	}
	m.quitTabsConfirm = false
	return m
}

func (m Model) requestQuit() (tea.Model, tea.Cmd) {
	live := false
	for index := range m.tabs {
		if m.tabs[index].session != nil || m.tabs[index].state == terminalTabStarting {
			live = true
			break
		}
	}
	if !live {
		return m, tea.Quit
	}
	if !m.quitTabsConfirm {
		m.quitTabsConfirm = true
		m.message = "Live terminal tabs are open · q/Ctrl+C again closes them and quits"
		return m, nil
	}
	m.quitAfterTabsClose = true
	m.message = "Closing terminal tabs…"
	cmds := make([]tea.Cmd, 0, len(m.tabs))
	kept := m.tabs[:0]
	for index := range m.tabs {
		tab := m.tabs[index]
		if tab.session == nil && tab.state != terminalTabStarting {
			continue
		}
		tab.state = terminalTabClosing
		tab.closePrompt = false
		if tab.session != nil {
			cmds = append(cmds, closeTerminalTabCmd(tab.id, tab.session))
		}
		kept = append(kept, tab)
	}
	m.tabs = kept
	m.activeTab = 0
	if len(m.tabs) == 0 {
		m.quitAfterTabsClose = false
		return m, tea.Quit
	}
	return m, tea.Batch(cmds...)
}

func (m Model) renderTabStrip(width int) string {
	items := []string{"1:Fleet"}
	for index := range m.tabs {
		state := "○"
		switch m.tabs[index].state {
		case terminalTabStarting, terminalTabFinishing:
			state = "◌"
		case terminalTabRunning:
			state = "●"
		case terminalTabExited:
			state = "✓"
		case terminalTabFailed:
			state = "!"
		case terminalTabClosing:
			state = "×"
		}
		label := fmt.Sprintf("%d:%s %s", index+2, truncate(m.tabs[index].label, 20), state)
		items = append(items, label)
	}
	renderItem := func(index int) string {
		item := items[index]
		marker := "  "
		if index == m.activeTab {
			marker = "› "
		}
		item = " " + marker + item + " "
		style := lipgloss.NewStyle().Foreground(colorText).Background(lipgloss.Color("236"))
		if index == m.activeTab {
			style = selected
		}
		return style.Render(item)
	}
	var output strings.Builder
	for index := range items {
		output.WriteString(renderItem(index))
		if index < len(items)-1 {
			output.WriteString(" ")
		}
	}
	line := output.String()
	if ansi.StringWidth(line) > width && m.activeTab > 0 {
		// Keep the permanent Fleet anchor and the active session visible even
		// when many tabs do not fit. A later tab must never disappear merely
		// because earlier labels consumed the strip.
		line = renderItem(0) + mutedStyle.Render(" … ") + renderItem(m.activeTab)
	}
	return padRight(truncate(line, width), width)
}

func (m Model) renderActiveTerminalTab(width, height int) string {
	index := m.activeTab - 1
	if index < 0 || index >= len(m.tabs) {
		return fitLines([]string{"Fleet"}, width, height)
	}
	tab := m.tabs[index]
	content := tab.screen
	if tab.session != nil && tab.state == terminalTabRunning {
		content = tab.session.Terminal.Render()
	}
	if content == "" {
		message := "Starting terminal session…"
		if tab.state == terminalTabFailed {
			message = "Terminal failed: " + tab.errorText
		}
		content = message
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	if tab.scrollOffset > 0 {
		history := tab.scrollback
		if tab.session != nil && tab.state == terminalTabRunning {
			history = terminalScrollbackSnapshot(tab.session)
		}
		combined := append(append([]string(nil), history...), lines...)
		end := max(0, len(combined)-min(tab.scrollOffset, len(history)))
		start := max(0, end-height)
		lines = combined[start:end]
	}
	if tab.state == terminalTabExited || tab.state == terminalTabFailed {
		status := "Session exited · Ctrl+] close · Ctrl+G Fleet"
		if tab.errorText != "" {
			status = "Session error: " + tab.errorText + " · Ctrl+] close · Ctrl+G Fleet"
		}
		lines = append(lines, mutedStyle.Render(truncate(status, width)))
	}
	if tab.closePrompt {
		warning := lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render("Live foreground process · Ctrl+] close · Esc cancel")
		if len(lines) == 0 {
			lines = append(lines, warning)
		} else {
			lines[0] = padRight(truncate(warning, width), width)
		}
	}
	return fitLines(lines, width, height)
}

func (m Model) renderTerminalTabFooter(width int) string {
	left := " TAB  Alt+1…9 select  Ctrl+N/P cycle  Ctrl+D close "
	if index := m.activeTab - 1; index >= 0 && index < len(m.tabs) && m.tabs[index].scrollOffset > 0 {
		available := len(m.tabs[index].scrollback)
		if m.tabs[index].session != nil && m.tabs[index].state == terminalTabRunning {
			available = m.tabs[index].session.Terminal.ScrollbackLen()
		}
		left = fmt.Sprintf(" SCROLL %d/%d  wheel ↓ live  key → live ", m.tabs[index].scrollOffset, available)
	}
	if index := m.activeTab - 1; index >= 0 && index < len(m.tabs) && m.tabs[index].closePrompt {
		left = " LIVE SESSION  Ctrl+] confirm close  Esc cancel "
	}
	right := ""
	if m.message != "" {
		right = truncate(session.CleanText(m.message), max(10, width/2)) + " "
	}
	space := max(1, width-ansi.StringWidth(left)-ansi.StringWidth(right))
	return statusStyle.Render(padRight(truncate(left+strings.Repeat(" ", space)+right, width), width))
}
