package ui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/containers"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/knownhosts"
	"github.com/ponomarev-prime/sshfleet-console/internal/localtarget"
	"github.com/ponomarev-prime/sshfleet-console/internal/openssh"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
	"github.com/ponomarev-prime/sshfleet-console/internal/session"
	"github.com/ponomarev-prime/sshfleet-console/internal/toolcheck"
	"github.com/ponomarev-prime/sshfleet-console/internal/workspace"
)

type focus int

const (
	focusSources focus = iota
	focusHosts
)

type Model struct {
	hosts           []inventory.Host
	sources         []inventory.SourceSummary
	client          openssh.Client
	refreshInterval time.Duration
	maxConcurrent   int
	sourcesWidthPct int
	previewWidthPct int
	hostColumnPct   int

	width                  int
	height                 int
	focus                  focus
	source                 int // 0 is All available; source N maps to sources[N-1].
	selected               int
	filter                 string
	filtering              bool
	searchReturnSourceName string
	searchReturnGroupName  string
	searchReturnHostID     string
	searchReturnSet        bool
	message                string
	results                map[string]probe.Result
	effective              map[string]openssh.Effective
	polling                map[string]bool
	sessionTail            map[string][]string
	hostKeyPlan            *knownhosts.Plan
	hostKeyIndex           int
	hostKeyBusy            bool
	hostKeyBackup          map[string]string
	hostKeyPrompt          map[string]bool
	queue                  []int
	active                 int
	lastRefresh            time.Time
	overridesDir           string
	appConfigPath          string
	editor                 string
	reload                 func() ([]inventory.Host, []inventory.SourceSummary, error)
	editing                bool
	refreshAfter           bool
	actionMenu             bool
	actionSelected         int
	actionHostIndex        int
	embedded               *session.Embedded
	embeddedIndex          int
	embeddedStarting       bool
	embeddedClosing        bool
	tabs                   []terminalTab
	activeTab              int // 0 is the permanent Fleet tab; N maps to tabs[N-1].
	nextTabID              uint64
	quitTabsConfirm        bool
	quitAfterTabsClose     bool
	targetStarting         bool
	workspaceStarting      bool
	health                 []toolcheck.Result
	editorHealth           toolcheck.Result
	healthVisible          bool
	groups                 []string
	groupDefinitions       []config.HostGroup
	groupsDir              string
	groupEditor            string
	groupReload            func() ([]inventory.Host, []inventory.SourceSummary, []config.HostGroup, error)
	groupDialog            groupDialogState
	commands               []config.Command
	groupCommandMenu       bool
	groupCommandChoice     int
	groupCommandConfirm    bool
	groupCommandRunning    bool
	groupResults           map[string]groupCommandResult
	dynamicRefresh         time.Duration
	dynamicDiscover        func() ([]inventory.Host, []inventory.SourceSummary)
	dynamicBusy            bool
}

type startPollMsg struct{}
type tickMsg time.Time
type dynamicTickMsg time.Time
type dynamicSourcesMsg struct {
	hosts   []inventory.Host
	sources []inventory.SourceSummary
}
type probeMsg struct {
	id     string
	result probe.Result
}
type resolveMsg struct {
	id        string
	effective openssh.Effective
}
type sessionFinishedMsg struct {
	index int
	err   error
	lines []string
}
type hostKeyPlanMsg struct {
	index     int
	plan      knownhosts.Plan
	effective openssh.Effective
	err       error
}
type hostKeyAppliedMsg struct {
	index  int
	result knownhosts.Applied
	err    error
}
type hostEditedMsg struct {
	id      string
	path    string
	hosts   []inventory.Host
	sources []inventory.SourceSummary
	err     error
}
type appEditedMsg struct {
	path string
	err  error
}
type sourceEditedMsg struct {
	id      string
	path    string
	hosts   []inventory.Host
	sources []inventory.SourceSummary
	err     error
}
type workspacePreparedMsg struct {
	index      int
	tool       workspace.Tool
	remotePath string
	err        error
}
type targetCommandPreparedMsg struct {
	index       int
	preview     bool
	label       string
	shell       string
	exitHint    string
	interruptOK bool
	cmd         *exec.Cmd
	err         error
}

type groupCommandResult struct {
	HostID   string
	Alias    string
	Preset   string
	Duration time.Duration
	Lines    []string
	Err      string
}

type groupCommandFinishedMsg struct {
	Group   string
	Preset  string
	Results []groupCommandResult
}

type Option func(*Model)

func WithHostEditor(overridesDir, editor string, reload func() ([]inventory.Host, []inventory.SourceSummary, error)) Option {
	return func(m *Model) {
		m.overridesDir = overridesDir
		m.editor = editor
		m.reload = reload
	}
}

func WithAppConfigEditor(path string) Option {
	return func(m *Model) {
		m.appConfigPath = path
	}
}

func WithUILayout(layout config.UIConfig) Option {
	return func(m *Model) {
		if layout.SourcesWidthPercent > 0 {
			m.sourcesWidthPct = layout.SourcesWidthPercent
		}
		if layout.PreviewWidthPercent > 0 {
			m.previewWidthPct = layout.PreviewWidthPercent
		}
		if layout.HostColumnPercent > 0 {
			m.hostColumnPct = layout.HostColumnPercent
		}
	}
}

func WithHealth(health []toolcheck.Result, editor toolcheck.Result) Option {
	return func(m *Model) {
		m.health = append([]toolcheck.Result(nil), health...)
		m.editorHealth = editor
	}
}

func WithGroupsAndCommands(groups []config.HostGroup, commands []config.Command) Option {
	return func(m *Model) {
		m.setGroupDefinitions(groups)
		m.commands = append([]config.Command(nil), commands...)
	}
}

func WithGroupEditor(dir, editor string, reload func() ([]inventory.Host, []inventory.SourceSummary, []config.HostGroup, error)) Option {
	return func(m *Model) {
		m.groupsDir = dir
		m.groupEditor = editor
		m.groupReload = reload
	}
}

func WithDynamicDiscovery(refresh time.Duration, discover func() ([]inventory.Host, []inventory.SourceSummary)) Option {
	return func(m *Model) {
		if refresh <= 0 {
			refresh = 2 * time.Second
		}
		m.dynamicRefresh = refresh
		m.dynamicDiscover = discover
	}
}

func New(hosts []inventory.Host, sources []inventory.SourceSummary, client openssh.Client, refresh time.Duration, maxConcurrent int, options ...Option) Model {
	if refresh <= 0 {
		refresh = 10 * time.Second
	}
	if maxConcurrent <= 0 {
		maxConcurrent = config.DefaultMaxConcurrent()
	}
	m := Model{
		hosts:           hosts,
		sources:         sources,
		client:          client,
		refreshInterval: refresh,
		maxConcurrent:   maxConcurrent,
		sourcesWidthPct: config.DefaultSourcesWidthPercent,
		previewWidthPct: config.DefaultPreviewWidthPercent,
		hostColumnPct:   config.DefaultHostColumnPercent,
		focus:           focusHosts,
		results:         make(map[string]probe.Result),
		effective:       make(map[string]openssh.Effective),
		polling:         make(map[string]bool),
		sessionTail:     make(map[string][]string),
		hostKeyIndex:    -1,
		hostKeyBackup:   make(map[string]string),
		hostKeyPrompt:   make(map[string]bool),
		groupResults:    make(map[string]groupCommandResult),
	}
	for _, option := range options {
		option(&m)
	}
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.tickCmd(), func() tea.Msg { return startPollMsg{} }}
	if m.dynamicDiscover != nil {
		cmds = append(cmds, m.dynamicTickCmd())
	}
	if len(m.hosts) > 0 {
		cmds = append(cmds, m.resolveCmd(0))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.embedded != nil {
			width, height, ok := m.previewTerminalDimensions()
			if ok {
				_ = m.embedded.Resize(width, height)
			}
		}
		width, height := m.fullTerminalDimensions()
		for index := range m.tabs {
			if m.tabs[index].session != nil && m.tabs[index].state == terminalTabRunning {
				_ = m.tabs[index].session.Resize(width, height)
			}
		}
		return m, nil

	case terminalTabStartedMsg:
		return m.applyTerminalTabStarted(msg)

	case terminalTabOutputMsg:
		return m.applyTerminalTabOutput(msg)

	case terminalTabFinishedMsg:
		return m.applyTerminalTabFinished(msg)

	case terminalTabClosedMsg:
		return m.applyTerminalTabClosed(msg)

	case embeddedStartedMsg:
		m.embeddedStarting = false
		if msg.err != nil {
			m.message = "Preview terminal: " + msg.err.Error()
			return m, nil
		}
		m.embedded = msg.session
		m.embeddedIndex = msg.index
		m.embeddedClosing = false
		m.message = "Preview terminal active · Ctrl+] close"
		return m, readEmbeddedCmd(msg.session)

	case embeddedOutputMsg:
		if m.embedded != msg.session {
			return m, nil
		}
		if len(msg.data) > 0 {
			_, _ = msg.session.Terminal.Write(msg.data)
		}
		if msg.err != nil {
			return m, finishEmbeddedCmd(msg.session, m.embeddedIndex)
		}
		return m, readEmbeddedCmd(msg.session)

	case embeddedFinishedMsg:
		if m.embedded != msg.session {
			return m, nil
		}
		if msg.index >= 0 && msg.index < len(m.hosts) && len(msg.lines) > 0 {
			m.sessionTail[m.hosts[msg.index].ID] = append([]string(nil), msg.lines...)
		}
		m.embedded = nil
		m.embeddedIndex = -1
		m.embeddedClosing = false
		if msg.forced {
			m.message = "Preview terminal closed"
		} else {
			m.message = "Preview terminal finished" + embeddedError(msg.err)
		}
		if msg.index >= 0 && msg.index < len(m.hosts) && m.hosts[msg.index].Probe && m.active == 0 {
			return m, func() tea.Msg { return startPollMsg{} }
		}
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{m.tickCmd()}
		if m.active == 0 && !m.editing {
			cmds = append(cmds, func() tea.Msg { return startPollMsg{} })
		} else if !m.editing {
			// Do not lose a refresh tick when a large fleet is still being
			// collected. The next sweep starts as soon as the bounded pool drains.
			m.refreshAfter = true
		}
		return m, tea.Batch(cmds...)

	case dynamicTickMsg:
		cmds := []tea.Cmd{m.dynamicTickCmd()}
		if m.dynamicDiscover != nil && !m.dynamicBusy && !m.editing && m.embedded == nil && !m.embeddedStarting && !m.targetStarting && m.active == 0 && len(m.queue) == 0 {
			m.dynamicBusy = true
			discover := m.dynamicDiscover
			cmds = append(cmds, func() tea.Msg {
				hosts, sources := discover()
				return dynamicSourcesMsg{hosts: hosts, sources: sources}
			})
		}
		return m, tea.Batch(cmds...)

	case dynamicSourcesMsg:
		m.dynamicBusy = false
		if m.editing || m.embedded != nil || m.embeddedStarting || m.targetStarting || m.active > 0 || len(m.queue) > 0 {
			return m, nil
		}
		selectedID := ""
		selectedSource := ""
		if m.source > 0 && m.source-1 < len(m.sources) {
			selectedSource = m.sources[m.source-1].Name
		}
		if index := m.selectedHostIndex(); index >= 0 && index < len(m.hosts) {
			selectedID = m.hosts[index].ID
		}
		failedSources := make(map[string]inventory.SourceSummary)
		for i := range msg.sources {
			if msg.sources[i].Err != nil {
				failedSources[msg.sources[i].Name] = msg.sources[i]
			}
		}
		staleCounts := make(map[string]int)
		keptHosts := make([]inventory.Host, 0, len(m.hosts)+len(msg.hosts))
		for _, host := range m.hosts {
			if host.TargetTransport() != inventory.TransportContainer {
				keptHosts = append(keptHosts, host)
				continue
			}
			if failed, ok := failedSources[host.SourceName]; ok {
				host.ContainerDiscoveryState = string(inventory.SourceStateStale)
				host.ContainerDiscoveryError = string(failed.State) + ": " + session.CleanText(failed.Err.Error())
				keptHosts = append(keptHosts, host)
				staleCounts[host.SourceName]++
			}
		}
		m.hosts = append(keptHosts, msg.hosts...)
		alive := make(map[string]struct{}, len(m.hosts))
		for _, host := range m.hosts {
			alive[host.ID] = struct{}{}
		}
		for id := range m.results {
			if strings.HasPrefix(id, "container:") {
				if _, ok := alive[id]; !ok {
					delete(m.results, id)
					delete(m.effective, id)
					delete(m.sessionTail, id)
					delete(m.polling, id)
				}
			}
		}
		sort.SliceStable(m.hosts, func(i, j int) bool {
			left, right := strings.ToLower(m.hosts[i].DisplayName()), strings.ToLower(m.hosts[j].DisplayName())
			if left == right {
				return m.hosts[i].ID < m.hosts[j].ID
			}
			return left < right
		})
		keptSources := make([]inventory.SourceSummary, 0, len(m.sources)+len(msg.sources))
		for _, source := range m.sources {
			if !source.Dynamic {
				keptSources = append(keptSources, source)
			}
		}
		for i := range msg.sources {
			if count := staleCounts[msg.sources[i].Name]; count > 0 {
				msg.sources[i].Hosts = count
				msg.sources[i].Detail = string(msg.sources[i].State) + ": " + session.CleanText(msg.sources[i].Err.Error())
				msg.sources[i].State = inventory.SourceStateStale
			}
		}
		m.sources = append(keptSources, msg.sources...)
		for _, source := range msg.sources {
			if source.Err != nil || source.State == inventory.SourceStatePartial || source.State == inventory.SourceStateStale {
				detail := source.Detail
				if detail == "" && source.Err != nil {
					detail = source.Err.Error()
				}
				m.message = source.Name + " · " + string(source.State) + ": " + session.CleanText(detail)
				break
			}
		}
		m.source = 0
		for i, source := range m.sources {
			if source.Name == selectedSource {
				m.source = i + 1
				break
			}
		}
		m.selected = 0
		for position, index := range m.visibleIndices() {
			if m.hosts[index].ID == selectedID {
				m.selected = position
				break
			}
		}
		return m, nil

	case startPollMsg:
		if m.editing {
			return m, nil
		}
		m.beginPoll()
		return m, m.fillPollSlots()

	case probeMsg:
		m.active--
		delete(m.polling, msg.id)
		if previous, ok := m.results[msg.id]; ok && msg.result.Status == probe.StatusOnline {
			msg.result.ApplyPrevious(previous)
		}
		m.results[msg.id] = msg.result
		if m.active == 0 && len(m.queue) == 0 {
			m.lastRefresh = time.Now()
		}
		if m.active == 0 && m.refreshAfter && !m.editing {
			m.refreshAfter = false
			return m, func() tea.Msg { return startPollMsg{} }
		}
		return m, m.fillPollSlots()

	case resolveMsg:
		m.effective[msg.id] = msg.effective
		return m, nil

	case hostKeyPlanMsg:
		m.hostKeyBusy = false
		if msg.err != nil {
			m.message = "Host key repair: " + msg.err.Error()
			return m, nil
		}
		if m.selectedHostIndex() != msg.index {
			m.message = "Host key inspection finished; select the host and press K again"
			return m, nil
		}
		m.effective[m.hosts[msg.index].ID] = msg.effective
		plan := msg.plan
		m.hostKeyPlan = &plan
		m.hostKeyIndex = msg.index
		m.message = "Сверьте fingerprint; Shift+K ещё раз создаст backup и удалит старую запись"
		return m, nil

	case hostKeyAppliedMsg:
		m.hostKeyBusy = false
		m.hostKeyPlan = nil
		m.hostKeyIndex = -1
		if msg.err != nil {
			m.message = "Host key repair failed: " + msg.err.Error()
			return m, nil
		}
		if msg.index >= 0 && msg.index < len(m.hosts) {
			hostID := m.hosts[msg.index].ID
			m.hostKeyBackup[hostID] = msg.result.BackupPath
			m.hostKeyPrompt[hostID] = true
		}
		m.message = "Старая запись удалена; Enter — подключиться и проверить новый fingerprint"
		return m, nil

	case sessionFinishedMsg:
		if msg.err != nil {
			m.message = "Сессия завершилась с ошибкой: " + msg.err.Error()
		} else {
			m.message = "Сессия завершена"
		}
		if msg.index >= 0 && msg.index < len(m.hosts) && len(msg.lines) > 0 {
			m.sessionTail[m.hosts[msg.index].ID] = append([]string(nil), msg.lines...)
		}
		if msg.err == nil && msg.index >= 0 && msg.index < len(m.hosts) {
			delete(m.hostKeyPrompt, m.hosts[msg.index].ID)
		}
		if msg.index >= 0 && msg.index < len(m.hosts) && m.hosts[msg.index].Probe && m.active == 0 {
			return m, func() tea.Msg { return startPollMsg{} }
		}
		return m, nil

	case hostEditedMsg:
		m.editing = false
		if msg.err != nil {
			m.message = "Host overlay: " + msg.err.Error()
			return m, nil
		}
		m.hosts = msg.hosts
		m.sources = msg.sources
		m.source = 0
		m.selected = 0
		for position, index := range m.visibleIndices() {
			if m.hosts[index].ID == msg.id {
				m.selected = position
				break
			}
		}
		delete(m.effective, msg.id)
		delete(m.results, msg.id)
		m.queue = nil
		m.message = "Overlay применён: " + msg.path
		cmds := []tea.Cmd{m.resolveSelectedCmd()}
		if m.active == 0 {
			cmds = append(cmds, func() tea.Msg { return startPollMsg{} })
		} else {
			m.refreshAfter = true
		}
		return m, tea.Batch(cmds...)

	case appEditedMsg:
		m.editing = false
		if msg.err != nil {
			m.message = "Application config: " + msg.err.Error()
			return m, nil
		}
		m.message = "Config saved; restart SSH Fleet Console to apply: " + msg.path
		if m.active == 0 && m.refreshAfter {
			m.refreshAfter = false
			return m, func() tea.Msg { return startPollMsg{} }
		}
		return m, nil

	case sourceEditedMsg:
		m.editing = false
		if msg.err != nil {
			m.message = "Source config: " + session.CleanText(msg.err.Error())
			return m, nil
		}
		m.hosts, m.sources = msg.hosts, msg.sources
		m.source, m.selected = 0, 0
		for position, index := range m.visibleIndices() {
			if m.hosts[index].ID == msg.id {
				m.selected = position
				break
			}
		}
		m.queue = nil
		m.message = "Source config reloaded: " + msg.path
		return m, tea.Batch(m.resolveSelectedCmd(), func() tea.Msg { return startPollMsg{} })

	case workspacePreparedMsg:
		m.workspaceStarting = false
		if msg.err != nil {
			m.message = "Bundled workspace: " + msg.err.Error()
			return m, nil
		}
		if msg.index < 0 || msg.index >= len(m.hosts) {
			m.message = "Bundled workspace: selected host disappeared"
			return m, nil
		}
		cmd, err := m.client.WorkspaceCommand(m.hosts[msg.index], msg.remotePath, msg.tool)
		if err != nil {
			m.message = "Bundled workspace: " + err.Error()
			return m, nil
		}
		m.message = "Starting bundled " + string(msg.tool) + " in a new tab…"
		return m.openTerminalTab(cmd, msg.index, "Workspace · "+m.hosts[msg.index].Alias, false)

	case targetCommandPreparedMsg:
		m.targetStarting = false
		if msg.err != nil {
			m.embeddedStarting = false
			m.message = session.CleanText(msg.err.Error())
			return m, nil
		}
		if msg.index < 0 || msg.index >= len(m.hosts) {
			m.embeddedStarting = false
			m.message = "Selected target disappeared"
			return m, nil
		}
		if msg.shell != "" && m.hosts[msg.index].TargetTransport() == inventory.TransportContainer {
			m.hosts[msg.index].ContainerShell = msg.shell
		}
		if msg.preview {
			width, height, ok := m.previewTerminalDimensions()
			if !ok {
				m.embeddedStarting = false
				m.message = "Preview terminal needs at least 62 columns"
				return m, nil
			}
			m.embeddedStarting = true
			m.message = "Starting " + msg.label + " in Preview…"
			return m, startEmbeddedCmd(msg.cmd, msg.index, msg.label, width, height)
		}
		m.message = "Starting " + msg.label + " in a new tab…"
		return m.openTerminalTab(msg.cmd, msg.index, msg.label, msg.interruptOK)

	case groupCommandFinishedMsg:
		m.groupCommandRunning = false
		failed := 0
		for _, result := range msg.Results {
			m.groupResults[result.HostID] = result
			if result.Err != "" {
				failed++
			}
		}
		m.message = fmt.Sprintf("Group %s · %s: %d hosts, %d failed", msg.Group, msg.Preset, len(msg.Results), failed)
		return m, nil

	case groupChangedMsg:
		return m.applyGroupChanged(msg)

	case tea.PasteMsg:
		return m.handlePaste(msg)

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.activeTab > 0 {
		return m.handleTerminalTabKey(msg)
	}
	if m.embedded != nil {
		return m.handleEmbeddedKey(msg)
	}
	if m.embeddedStarting {
		if msg.String() == "ctrl+c" {
			return m.requestQuit()
		}
		return m, nil
	}
	if m.targetStarting {
		if msg.String() == "ctrl+c" {
			return m.requestQuit()
		}
		return m, nil
	}
	if m.workspaceStarting {
		if msg.String() == "ctrl+c" {
			return m.requestQuit()
		}
		return m, nil
	}
	if len(m.tabs) > 0 {
		if next, ok := m.activateDirectTab(msg); ok {
			return next, nil
		}
		switch msg.String() {
		case "ctrl+n":
			return m.activateNextTab(), nil
		case "ctrl+p":
			return m.activatePreviousTab(), nil
		case "ctrl+g":
			m.activeTab = 0
			return m, nil
		}
	}
	if m.healthVisible {
		if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" {
			m.healthVisible = false
		}
		return m, nil
	}
	if m.groupDialog.mode != groupDialogNone {
		return m.handleGroupDialogKey(msg)
	}
	if m.groupCommandMenu {
		return m.handleGroupCommandKey(msg)
	}
	if m.actionMenu {
		return m.handleActionMenuKey(msg)
	}
	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			if m.filter == "" {
				m.restoreSearchContext()
			}
		case "enter":
			m.filtering = false
			if strings.TrimSpace(m.filter) == "" {
				m.restoreSearchContext()
			}
		case "backspace":
			runes := []rune(m.filter)
			if len(runes) > 0 {
				m.filter = string(runes[:len(runes)-1])
			}
			m.selected = 0
		case "ctrl+u":
			m.filter = ""
			m.selected = 0
		case "ctrl+c":
			return m.requestQuit()
		default:
			cleaned := strings.ReplaceAll(session.CleanText(msg.Text), "\n", "")
			if cleaned != "" {
				m.filter += cleaned
				m.selected = 0
			}
		}
		return m, m.resolveSelectedCmd()
	}
	if m.hostKeyPlan != nil || m.hostKeyBusy {
		switch msg.String() {
		case "q", "ctrl+c":
			return m.requestQuit()
		case "esc":
			if !m.hostKeyBusy {
				m.hostKeyPlan = nil
				m.hostKeyIndex = -1
				m.message = "Host key repair отменён"
			}
			return m, nil
		case "K":
			if m.hostKeyBusy {
				return m, nil
			}
			m.hostKeyBusy = true
			m.message = "Создаю backup и удаляю старую запись…"
			return m, applyHostKeyPlanCmd(m.hostKeyIndex, *m.hostKeyPlan)
		default:
			m.message = "Shift+K — backup и удаление; Esc — отмена"
			return m, nil
		}
	}

	visible := m.visibleIndices()
	switch msg.String() {
	case "q", "ctrl+c":
		return m.requestQuit()
	case "?":
		m.healthVisible = true
		return m, nil
	case "x":
		if m.selectedGroup() == "" {
			m.message = "Select a group in GROUPS first"
			return m, nil
		}
		if len(m.commands) == 0 {
			m.message = "No [[command_presets]] configured"
			return m, nil
		}
		m.groupCommandMenu = true
		m.groupCommandChoice = 0
		m.groupCommandConfirm = false
		return m, nil
	case "n":
		if m.focus == focusSources {
			return m.openCreateGroup()
		}
	case "R":
		if m.focus == focusSources {
			return m.openRenameGroup()
		}
	case "D":
		if m.focus == focusSources {
			return m.openDeleteGroup()
		}
	case "m":
		if m.focus == focusHosts {
			return m.openMembershipGroup()
		}
	case "/":
		m.beginGlobalSearch()
		return m, nil
	case "tab":
		if m.focus == focusSources {
			m.focus = focusHosts
			return m, m.resolveSelectedCmd()
		}
		m.focus = focusSources
		return m, nil
	case "h", "left":
		m.focus = focusSources
		return m, nil
	case "l", "right":
		m.focus = focusHosts
		return m, m.resolveSelectedCmd()
	case "esc":
		if m.filter != "" {
			m.restoreSearchContext()
			return m, m.resolveSelectedCmd()
		}
	case "j", "down":
		if m.focus == focusSources {
			if m.source < len(m.sources)+len(m.groups) {
				m.source++
				m.selected = 0
			}
			return m, m.resolveSelectedCmd()
		}
		if m.selected < len(visible)-1 {
			m.selected++
		}
		return m, m.resolveSelectedCmd()
	case "k", "up":
		if m.focus == focusSources {
			if m.source > 0 {
				m.source--
				m.selected = 0
			}
			return m, m.resolveSelectedCmd()
		}
		if m.selected > 0 {
			m.selected--
		}
		return m, m.resolveSelectedCmd()
	case "g", "home":
		if m.focus == focusSources {
			m.source = 0
		}
		m.selected = 0
		return m, m.resolveSelectedCmd()
	case "G", "end":
		if m.focus == focusSources {
			m.source = len(m.sources) + len(m.groups)
			m.selected = 0
			return m, m.resolveSelectedCmd()
		}
		if len(visible) > 0 {
			m.selected = len(visible) - 1
		}
		return m, m.resolveSelectedCmd()
	case "r":
		if m.active == 0 {
			return m, func() tea.Msg { return startPollMsg{} }
		}
		m.message = "Обновление уже выполняется"
	case "e":
		if m.focus == focusSources && m.selectedGroup() != "" {
			return m.editSelectedGroup()
		}
		if m.focus != focusHosts || len(visible) == 0 {
			return m, nil
		}
		index := visible[min(m.selected, len(visible)-1)]
		return m.executeHostAction(index, actionEditHost)
	case "c":
		path, err := config.EnsureAppConfig(m.appConfigPath)
		if err != nil {
			m.message = "Application config: " + err.Error()
			return m, nil
		}
		cmd, err := editorCommand(m.editor, path)
		if err != nil {
			m.message = "Application config: " + err.Error()
			return m, nil
		}
		m.editing = true
		m.queue = nil
		m.polling = make(map[string]bool)
		m.refreshAfter = m.active > 0
		m.message = "Editing application config: " + path
		return m, editAppConfigCmd(cmd, path)
	case "K":
		if m.focus != focusHosts || len(visible) == 0 {
			return m, nil
		}
		index := visible[min(m.selected, len(visible)-1)]
		hostID := m.hosts[index].ID
		if m.hostKeyPrompt[hostID] {
			m.message = "Старая запись уже удалена; Enter — проверить и принять новый fingerprint"
			return m, nil
		}
		result, ok := m.results[hostID]
		if !ok || result.Status != probe.StatusHostKey {
			m.message = "Для выбранного узла нет ошибки host key"
			return m, nil
		}
		m.hostKeyBusy = true
		m.message = "Проверяю known_hosts и сохранённый fingerprint…"
		return m, m.inspectHostKeyCmd(index, result)
	case "enter":
		if m.focus == focusSources {
			m.focus = focusHosts
			return m, m.resolveSelectedCmd()
		}
		if len(visible) == 0 {
			return m, nil
		}
		m.actionMenu = true
		m.actionSelected = 0
		m.actionHostIndex = visible[min(m.selected, len(visible)-1)]
		return m, nil
	}
	return m, nil
}

func (m Model) selectedHostIndex() int {
	if m.focus != focusHosts {
		return -1
	}
	visible := m.visibleIndices()
	if len(visible) == 0 {
		return -1
	}
	return visible[min(m.selected, len(visible)-1)]
}

func (m *Model) beginPoll() {
	if m.active != 0 {
		return
	}
	m.queue = m.queue[:0]
	for i, host := range m.hosts {
		if !host.Probe {
			continue
		}
		m.queue = append(m.queue, i)
		m.polling[host.ID] = true
	}
}

func (m *Model) fillPollSlots() tea.Cmd {
	var cmds []tea.Cmd
	for m.active < m.maxConcurrent && len(m.queue) > 0 {
		index := m.queue[0]
		m.queue = m.queue[1:]
		m.active++
		cmds = append(cmds, m.probeCmd(index))
	}
	return tea.Batch(cmds...)
}

func (m Model) probeCmd(index int) tea.Cmd {
	host := m.hosts[index]
	client := m.client
	return func() tea.Msg {
		timeout := client.ConnectTimeout + 4*time.Second
		if timeout <= 4*time.Second {
			timeout = 10 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		var result probe.Result
		switch host.TargetTransport() {
		case inventory.TransportLocal:
			result = localtarget.Probe(ctx, host)
		case inventory.TransportContainer:
			result = containers.Probe(host)
		default:
			result = client.Probe(ctx, host)
		}
		return probeMsg{id: host.ID, result: result}
	}
}

func (m Model) resolveCmd(index int) tea.Cmd {
	host := m.hosts[index]
	client := m.client
	return func() tea.Msg {
		if host.TargetTransport() != inventory.TransportSSH {
			return resolveMsg{id: host.ID}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		return resolveMsg{id: host.ID, effective: client.Resolve(ctx, host)}
	}
}

func (m Model) resolveSelectedCmd() tea.Cmd {
	visible := m.visibleIndices()
	if len(visible) == 0 {
		return nil
	}
	selected := min(m.selected, len(visible)-1)
	return m.resolveCmd(visible[selected])
}

func (m Model) inspectHostKeyCmd(index int, result probe.Result) tea.Cmd {
	host := m.hosts[index]
	client := m.client
	effective := m.effective[host.ID]
	return func() tea.Msg {
		if effective.Hostname == "" && effective.Error == "" {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			effective = client.Resolve(ctx, host)
		}
		if effective.Error != "" {
			return hostKeyPlanMsg{index: index, effective: effective, err: fmt.Errorf("resolve SSH settings: %s", effective.Error)}
		}
		plan, err := knownhosts.Inspect(
			effective.KnownHostsLookup(),
			effective.UserKnownHostsFiles,
			result.KnownHostsFile,
			result.KnownHostsLine,
			result.PresentedHostKey,
		)
		return hostKeyPlanMsg{index: index, plan: plan, effective: effective, err: err}
	}
}

func applyHostKeyPlanCmd(index int, plan knownhosts.Plan) tea.Cmd {
	return func() tea.Msg {
		result, err := knownhosts.Apply(plan)
		return hostKeyAppliedMsg{index: index, result: result, err: err}
	}
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(m.refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) dynamicTickCmd() tea.Cmd {
	return tea.Tick(m.dynamicRefresh, func(t time.Time) tea.Msg { return dynamicTickMsg(t) })
}

func interruptedExit(err error) bool {
	if err == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(err.Error()), "signal: interrupt") {
		return true
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		return false
	}
	// Docker commonly maps an intentional Ctrl+C while following logs to 130;
	// rootless Podman currently maps the same PTY interaction to 1. This helper
	// is consulted only for commands explicitly marked interruptOK (follow
	// logs), never for shells or arbitrary commands.
	return exitError.ExitCode() == 130 || exitError.ExitCode() == 1
}

func gitSessionCmd(cmd *exec.Cmd, index int, alias string) tea.Cmd {
	capture := session.NewCapture(128 * 1024)
	wrapped := &session.Command{
		Cmd:     cmd,
		Capture: capture,
		Banner:  fmt.Sprintf("── SSH Fleet Console · Git access · %s ──", alias),
	}
	return tea.Exec(wrapped, func(err error) tea.Msg {
		lines := capture.LastLines(12)
		if gitSessionSucceeded(lines) {
			err = nil
		}
		return sessionFinishedMsg{index: index, err: err, lines: lines}
	})
}

func gitSessionSucceeded(lines []string) bool {
	text := strings.ToLower(strings.Join(lines, "\n"))
	return strings.Contains(text, "successfully authenticated") ||
		strings.Contains(text, "welcome to gitlab") ||
		strings.Contains(text, "does not provide shell access") ||
		strings.Contains(text, "shell access is disabled")
}

func editHostCmd(cmd *exec.Cmd, id, path string, reload func() ([]inventory.Host, []inventory.SourceSummary, error)) tea.Cmd {
	// Editors must receive the terminal file descriptors directly. Wrapping
	// stdout/stderr in an io.MultiWriter makes them pipes, which breaks terminal
	// capability detection and causes cursor-key escape sequences to be echoed
	// literally by full-screen editors such as Neovim.
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return hostEditedMsg{id: id, path: path, err: fmt.Errorf("editor: %w", err)}
		}
		hosts, sources, err := reload()
		return hostEditedMsg{id: id, path: path, hosts: hosts, sources: sources, err: err}
	})
}

func editAppConfigCmd(cmd *exec.Cmd, path string) tea.Cmd {
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return appEditedMsg{path: path, err: fmt.Errorf("editor: %w", err)}
		}
		return appEditedMsg{path: path}
	})
}

func editSourceCmd(cmd *exec.Cmd, id, path string, reload func() ([]inventory.Host, []inventory.SourceSummary, error)) tea.Cmd {
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return sourceEditedMsg{id: id, path: path, err: fmt.Errorf("editor: %w", err)}
		}
		hosts, sources, err := reload()
		return sourceEditedMsg{id: id, path: path, hosts: hosts, sources: sources, err: err}
	})
}

func editorCommand(configured, path string) (*exec.Cmd, error) {
	editor := strings.TrimSpace(configured)
	if editor == "" {
		return nil, fmt.Errorf("no editor available; install nvim, vim, or nano, or configure app.editor_priority")
	}
	if strings.ContainsAny(editor, " \t\r\n") {
		return nil, fmt.Errorf("editor must be one executable without arguments; use a wrapper script: %q", editor)
	}
	resolved, err := exec.LookPath(editor)
	if err != nil {
		return nil, fmt.Errorf("find editor %q: %w", editor, err)
	}
	return exec.Command(resolved, path), nil
}

func (m Model) visibleIndices() []int {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	sourceName := ""
	if m.source > 0 && m.source <= len(m.sources) {
		sourceName = m.sources[m.source-1].Name
	}
	groupName := m.selectedGroup()
	visible := make([]int, 0, len(m.hosts))
	for i, host := range m.hosts {
		if query == "" {
			if sourceName != "" && host.SourceName != sourceName {
				continue
			}
			if groupName != "" && !contains(host.Groups, groupName) {
				continue
			}
			visible = append(visible, i)
			continue
		}
		if matchesHostSearch(searchText(host, m.effective[host.ID]), query) {
			visible = append(visible, i)
		}
	}
	return visible
}

func (m *Model) beginGlobalSearch() {
	if !m.searchReturnSet && strings.TrimSpace(m.filter) == "" {
		if m.source > 0 && m.source <= len(m.sources) {
			m.searchReturnSourceName = m.sources[m.source-1].Name
		} else {
			m.searchReturnGroupName = m.selectedGroup()
		}
		visible := m.visibleIndices()
		if len(visible) > 0 {
			m.searchReturnHostID = m.hosts[visible[min(m.selected, len(visible)-1)]].ID
		}
		m.searchReturnSet = true
	}
	m.source = 0
	m.selected = 0
	m.focus = focusHosts
	m.filtering = true
}

func (m *Model) restoreSearchContext() {
	m.filter = ""
	m.filtering = false
	if !m.searchReturnSet {
		m.selected = 0
		return
	}
	m.source = 0
	if m.searchReturnSourceName != "" {
		for index, source := range m.sources {
			if source.Name == m.searchReturnSourceName {
				m.source = index + 1
				break
			}
		}
	} else if m.searchReturnGroupName != "" {
		for index, group := range m.groups {
			if group == m.searchReturnGroupName {
				m.source = len(m.sources) + index + 1
				break
			}
		}
	}
	m.selected = 0
	if m.searchReturnHostID != "" {
		for position, index := range m.visibleIndices() {
			if m.hosts[index].ID == m.searchReturnHostID {
				m.selected = position
				break
			}
		}
	}
	m.searchReturnSourceName = ""
	m.searchReturnGroupName = ""
	m.searchReturnHostID = ""
	m.searchReturnSet = false
}

func (m Model) selectedGroup() string {
	index := m.source - len(m.sources) - 1
	if index < 0 || index >= len(m.groups) {
		return ""
	}
	return m.groups[index]
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (m Model) handleGroupCommandKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if len(m.commands) == 0 || m.selectedGroup() == "" {
		m.groupCommandMenu = false
		return m, nil
	}
	switch msg.String() {
	case "esc", "q":
		if m.groupCommandConfirm {
			m.groupCommandConfirm = false
		} else {
			m.groupCommandMenu = false
		}
		return m, nil
	case "j", "down":
		if !m.groupCommandConfirm && m.groupCommandChoice < len(m.commands)-1 {
			m.groupCommandChoice++
		}
		return m, nil
	case "k", "up":
		if !m.groupCommandConfirm && m.groupCommandChoice > 0 {
			m.groupCommandChoice--
		}
		return m, nil
	case "enter":
		if !m.groupCommandConfirm {
			m.groupCommandConfirm = true
			return m, nil
		}
		group := m.selectedGroup()
		preset := m.commands[min(m.groupCommandChoice, len(m.commands)-1)]
		indices := m.groupHostIndices()
		if len(indices) == 0 {
			m.groupCommandMenu = false
			m.groupCommandConfirm = false
			m.message = "Group " + group + " has no hosts; command was not started"
			return m, nil
		}
		hosts := make([]inventory.Host, 0, len(indices))
		for _, index := range indices {
			hosts = append(hosts, m.hosts[index])
		}
		m.groupCommandMenu = false
		m.groupCommandConfirm = false
		m.groupCommandRunning = true
		m.message = fmt.Sprintf("Running %s on group %s (%d hosts)…", preset.Name, group, len(hosts))
		return m, runGroupCommandCmd(m.client, group, preset, hosts, m.maxConcurrent)
	}
	return m, nil
}

func (m Model) groupHostIndices() []int {
	group := m.selectedGroup()
	if group == "" {
		return nil
	}
	indices := make([]int, 0)
	for index, host := range m.hosts {
		if contains(host.Groups, group) {
			indices = append(indices, index)
		}
	}
	return indices
}

func runGroupCommandCmd(client openssh.Client, group string, preset config.Command, hosts []inventory.Host, defaultConcurrent int) tea.Cmd {
	return func() tea.Msg {
		limit := preset.MaxConcurrent
		if limit <= 0 {
			limit = defaultConcurrent
		}
		limit = max(1, min(limit, max(1, len(hosts))))
		semaphore := make(chan struct{}, limit)
		results := make([]groupCommandResult, len(hosts))
		var wait sync.WaitGroup
		for index := range hosts {
			index := index
			wait.Add(1)
			go func() {
				defer wait.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				host := hosts[index]
				started := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), preset.Timeout.Duration)
				defer cancel()
				cmd, err := targetCommand(ctx, client, host, preset.Argv)
				capture := session.NewCapture(64 * 1024)
				if err == nil {
					cmd.Stdout, cmd.Stderr = capture, capture
					err = cmd.Run()
				}
				result := groupCommandResult{HostID: host.ID, Alias: host.Alias, Preset: preset.Name, Duration: time.Since(started), Lines: capture.LastLines(12)}
				if err != nil {
					result.Err = session.CleanText(err.Error())
				}
				results[index] = result
			}()
		}
		wait.Wait()
		return groupCommandFinishedMsg{Group: group, Preset: preset.Name, Results: results}
	}
}

func targetCommand(ctx context.Context, client openssh.Client, host inventory.Host, argv []string) (*exec.Cmd, error) {
	switch host.TargetTransport() {
	case inventory.TransportLocal:
		return localtarget.Command(ctx, host, argv)
	case inventory.TransportContainer:
		return containers.Command(ctx, host, argv)
	default:
		return client.Command(ctx, host, argv)
	}
}

func searchText(host inventory.Host, effective openssh.Effective) string {
	return strings.Join([]string{
		host.ID,
		host.Alias,
		host.Name,
		host.SourceName,
		host.Hostname,
		host.User,
		fmt.Sprint(host.Port),
		host.ProxyJump,
		strings.Join(host.Tags, " "),
		strings.Join(host.Groups, " "),
		string(host.TargetTransport()),
		host.ContainerRuntime,
		host.ContainerID,
		host.ContainerImage,
		host.ContainerStatus,
		effective.Hostname,
		effective.User,
		effective.Port,
		effective.ProxyJump,
	}, " ")
}

func matchesHostSearch(haystack, query string) bool {
	haystack = strings.ToLower(haystack)
	for _, term := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}
