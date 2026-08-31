package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ponomarev-prime/sshfleet-console/internal/config"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/probe"
	"github.com/ponomarev-prime/sshfleet-console/internal/session"
)

var (
	colorBlue   = lipgloss.Color("39")
	colorGreen  = lipgloss.Color("42")
	colorYellow = lipgloss.Color("214")
	colorRed    = lipgloss.Color("203")
	colorPurple = lipgloss.Color("171")
	colorMuted  = lipgloss.Color("244")
	colorText   = lipgloss.Color("252")
	selected    = lipgloss.NewStyle().Background(lipgloss.Color("25")).Foreground(lipgloss.Color("231")).Bold(true)
	selectedDim = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("255")).Bold(true)
	// dtop keeps the selected row neutral so the resource columns remain the
	// visual signal. Host bars use glyphs instead of background-only spaces,
	// therefore their filled/empty shape survives the row highlight as well.
	selectedHost    = lipgloss.NewStyle().Background(lipgloss.Color("238")).Bold(true)
	selectedHostDim = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true)
	titleStyle      = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	mutedStyle      = lipgloss.NewStyle().Foreground(colorMuted)
	statusStyle     = lipgloss.NewStyle().Foreground(colorText).Background(lipgloss.Color("236"))
	divider         = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)

func (m Model) View() tea.View {
	width, height := m.width, m.height
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}
	bodyHeight := max(3, height-2)
	if m.activeTab > 0 {
		content := m.renderTabStrip(width) + "\n" + m.renderActiveTerminalTab(width, bodyHeight)
		v := tea.NewView(content + "\n" + m.renderTerminalTabFooter(width))
		v.AltScreen = true
		v.WindowTitle = "SSH Fleet Console · terminal tab"
		return v
	}

	var content string
	switch {
	case width >= 100:
		leftWidth, middleWidth, rightWidth := m.widePaneWidths(width)
		content = lipgloss.JoinHorizontal(lipgloss.Top,
			m.flatPane("SOURCES", m.renderSources(leftWidth, bodyHeight-1), leftWidth, bodyHeight, m.focus == focusSources),
			verticalDivider(bodyHeight),
			m.flatPane("HOSTS", m.renderHosts(middleWidth, bodyHeight-1), middleWidth, bodyHeight, m.focus == focusHosts),
			verticalDivider(bodyHeight),
			m.flatPane("PREVIEW", m.renderPreview(rightWidth, bodyHeight-1), rightWidth, bodyHeight, false),
		)
	case width >= 62:
		if m.focus == focusSources {
			leftWidth, rightWidth := sourceAndHostPaneWidths(width, m.sourcesWidthPct)
			content = lipgloss.JoinHorizontal(lipgloss.Top,
				m.flatPane("SOURCES", m.renderSources(leftWidth, bodyHeight-1), leftWidth, bodyHeight, true),
				verticalDivider(bodyHeight),
				m.flatPane("HOSTS", m.renderHosts(rightWidth, bodyHeight-1), rightWidth, bodyHeight, false),
			)
		} else {
			leftWidth, rightWidth := m.hostAndPreviewPaneWidths(width)
			content = lipgloss.JoinHorizontal(lipgloss.Top,
				m.flatPane("HOSTS", m.renderHosts(leftWidth, bodyHeight-1), leftWidth, bodyHeight, true),
				verticalDivider(bodyHeight),
				m.flatPane("PREVIEW", m.renderPreview(rightWidth, bodyHeight-1), rightWidth, bodyHeight, false),
			)
		}
	default:
		if m.focus == focusSources {
			content = m.flatPane("SOURCES", m.renderSources(width, bodyHeight-1), width, bodyHeight, true)
		} else {
			content = m.flatPane("HOSTS", m.renderHosts(width, bodyHeight-1), width, bodyHeight, true)
		}
	}
	if m.actionMenu {
		content = m.overlayActionMenu(content, width, bodyHeight)
	}
	if m.groupCommandMenu {
		content = m.overlayGroupCommandMenu(content, width, bodyHeight)
	}
	if m.groupDialog.mode != groupDialogNone {
		content = m.overlayGroupDialog(content, width, bodyHeight)
	}
	if m.healthVisible {
		content = m.overlayHealth(content, width, bodyHeight)
	}

	v := tea.NewView(m.renderTabStrip(width) + "\n" + content + "\n" + m.renderFooter(width))
	v.AltScreen = true
	v.WindowTitle = "SSH Fleet Console"
	return v
}

func sourceAndHostPaneWidths(width, sourcesPercent int) (sources, hosts int) {
	sources = responsiveSideWidth(width, sourcesPercent, 18, 30)
	hosts = max(1, width-sources-1)
	return sources, hosts
}

func hostAndPreviewPaneWidths(width, previewPercent int) (hosts, preview int) {
	// In two-pane mode HOSTS remains the workspace rather than becoming the
	// narrow navigation column. Preview is useful context, but it must not take
	// more room than the fleet table it describes.
	preview = responsiveSideWidth(width, previewPercent, 22, 35)
	hosts = max(1, width-preview-1)
	return hosts, preview
}

func widePaneWidths(width, sourcesPercent, previewPercent int) (left, middle, right int) {
	// Sources are navigation, Preview is detail, and the dtop-like host table
	// benefits most from horizontal space. Percentages come from [app.ui], while
	// the responsive floors keep labels usable on smaller terminals.
	left = responsiveSideWidth(width, sourcesPercent, 18, 30)
	right = responsiveSideWidth(width, previewPercent, 22, 35)
	middle = max(42, width-left-right-2)
	return left, middle, right
}

func (m Model) widePaneWidths(width int) (left, middle, right int) {
	left, middle, right = widePaneWidths(width, m.sourcesWidthPct, m.previewWidthPct)
	if m.embedded == nil && !m.embeddedStarting {
		return left, middle, right
	}
	right = min(width-left-2-42, max(right, width*45/100))
	middle = max(42, width-left-right-2)
	return left, middle, right
}

func (m Model) hostAndPreviewPaneWidths(width int) (hosts, preview int) {
	hosts, preview = hostAndPreviewPaneWidths(width, m.previewWidthPct)
	if m.embedded == nil && !m.embeddedStarting {
		return hosts, preview
	}
	preview = min(width-32, max(preview, width*55/100))
	hosts = max(31, width-preview-1)
	return hosts, preview
}

func responsiveSideWidth(width, percent, minimum, maximumPercent int) int {
	if percent <= 0 {
		return minimum
	}
	return max(minimum, min(width*maximumPercent/100, width*percent/100))
}

func (m Model) previewTerminalDimensions() (width, height int, ok bool) {
	terminalWidth := m.width
	terminalHeight := m.height
	if terminalWidth <= 0 {
		terminalWidth = 100
	}
	if terminalHeight <= 0 {
		terminalHeight = 30
	}
	bodyHeight := max(3, terminalHeight-2)
	switch {
	case terminalWidth >= 100:
		_, _, width = m.widePaneWidths(terminalWidth)
	case terminalWidth >= 62 && m.focus == focusHosts:
		_, width = m.hostAndPreviewPaneWidths(terminalWidth)
	default:
		return 0, 0, false
	}
	// One row belongs to the pane heading and one to the embedded-session
	// banner. The emulator owns all remaining cells.
	return max(1, width), max(1, bodyHeight-2), true
}

func (m Model) flatPane(title, content string, width, height int, focused bool) string {
	if width <= 0 {
		return ""
	}
	header := titleStyle.Render(" " + title + " ")
	lineStyle := titleStyle
	if focused {
		header = selected.Render(" " + title + " ")
		lineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("31"))
	}
	header += lineStyle.Render(strings.Repeat("─", max(0, width-ansi.StringWidth(header))))
	lines := strings.Split(content, "\n")
	if content == "" {
		lines = nil
	}
	lines = append([]string{header}, lines...)
	return fitLines(lines, width, height)
}

func (m Model) renderSources(width, height int) string {
	type item struct {
		name  string
		count int
		err   error
		state inventory.SourceState
	}
	items := []item{{name: "All available", count: len(m.hosts)}}
	for _, source := range m.sources {
		items = append(items, item{name: source.Name, count: source.Hosts, err: source.Err, state: source.State})
	}
	type row struct {
		selection int
		text      string
	}
	renderItem := func(position int, entry item, group bool) string {
		icon := lipgloss.NewStyle().Foreground(colorGreen).Render("●")
		count := fmt.Sprintf("%d", entry.count)
		if position == 0 {
			icon = lipgloss.NewStyle().Foreground(colorBlue).Render("◆")
		}
		switch entry.state {
		case inventory.SourceStateEmpty:
			icon = mutedStyle.Render("○")
			count = "empty"
		case inventory.SourceStatePartial:
			icon = lipgloss.NewStyle().Foreground(colorYellow).Render("◐")
			count = fmt.Sprintf("~%d", entry.count)
		case inventory.SourceStateStale:
			icon = lipgloss.NewStyle().Foreground(colorYellow).Render("◌")
			count = "stale"
		case inventory.SourceStateDenied:
			icon = lipgloss.NewStyle().Foreground(colorRed).Render("●")
			count = "denied"
		case inventory.SourceStateUnavailable:
			icon = lipgloss.NewStyle().Foreground(colorRed).Render("●")
			count = "down"
		}
		if entry.err != nil && entry.state == "" {
			icon = lipgloss.NewStyle().Foreground(colorRed).Render("●")
			count = "error"
		}
		if group {
			icon = lipgloss.NewStyle().Foreground(colorPurple).Render("◇")
			if entry.count == 0 {
				icon = mutedStyle.Render("◇")
			}
		}
		nameWidth := max(1, width-ansi.StringWidth(icon)-ansi.StringWidth(count)-3)
		line := icon + " " + padRight(truncate(session.CleanText(entry.name), nameWidth), nameWidth) + " " + count
		line = padRight(truncate(line, width), width)
		if position == m.source {
			if m.focus == focusSources {
				line = selected.Render(ansi.Strip(line))
			} else {
				line = selectedDim.Render(ansi.Strip(line))
			}
		}
		return line
	}
	rows := make([]row, 0, len(items)+len(m.groups)+2)
	for position, entry := range items {
		rows = append(rows, row{selection: position, text: renderItem(position, entry, false)})
	}
	groupHeader := titleStyle.Render(" GROUPS ")
	groupHeader += titleStyle.Render(strings.Repeat("─", max(0, width-ansi.StringWidth(groupHeader))))
	rows = append(rows, row{selection: -1, text: groupHeader})
	for index, group := range m.groups {
		count := 0
		for _, host := range m.hosts {
			if contains(host.Groups, group) {
				count++
			}
		}
		position := len(items) + index
		rows = append(rows, row{selection: position, text: renderItem(position, item{name: group, count: count}, true)})
	}
	if len(m.groups) == 0 {
		rows = append(rows, row{selection: -1, text: mutedStyle.Render(truncate("  n create group", width))})
	}
	selectedRow := 0
	for index, candidate := range rows {
		if candidate.selection == m.source {
			selectedRow = index
			break
		}
	}
	start := max(0, selectedRow-height/2)
	if start+height > len(rows) {
		start = max(0, len(rows)-height)
	}
	end := min(len(rows), start+height)
	lines := make([]string, 0, end-start)
	for _, candidate := range rows[start:end] {
		lines = append(lines, candidate.text)
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderHosts(width, height int) string {
	visible := m.visibleIndices()
	if len(visible) == 0 {
		if m.filter != "" {
			return mutedStyle.Render(truncate("No hosts match /"+m.filter, width))
		}
		return mutedStyle.Render("No hosts in this source")
	}
	layout := newHostTableLayout(width, m.hostColumnPct)
	selectedPos := min(m.selected, len(visible)-1)
	capacity := max(1, (height-1)/2)
	start := max(0, selectedPos-capacity/2)
	if start+capacity > len(visible) {
		start = max(0, len(visible)-capacity)
	}
	end := min(len(visible), start+capacity)

	lines := []string{renderHostTableHeader(layout)}
	for pos := start; pos < end; pos++ {
		host := m.hosts[visible[pos]]
		result, ok := m.results[host.ID]
		icon := m.statusIcon(host, result, ok)
		metrics := renderHostMetrics(result, ok, layout)
		line1 := icon + " " + padRight(truncate(session.CleanText(host.DisplayName()), layout.nameWidth), layout.nameWidth) + " " + metrics
		line1 = padRight(truncate(line1, width), width)
		line2 := mutedStyle.Render(truncate("  "+session.CleanText(processText(host, result, ok)), width))
		if pos == selectedPos {
			if m.focus == focusHosts {
				line1 = selectedHost.Render(ansi.Strip(line1))
			} else {
				line1 = selectedHostDim.Render(ansi.Strip(line1))
			}
		}
		lines = append(lines, line1, line2)
	}
	return strings.Join(lines, "\n")
}

type hostTableLayout struct {
	width       int
	nameWidth   int
	cpuWidth    int
	memoryWidth int
	swapWidth   int
	usageWidth  int
	showUsage   bool
}

func newHostTableLayout(width int, configuredPercent ...int) hostTableLayout {
	hostPercent := config.DefaultHostColumnPercent
	if len(configuredPercent) > 0 && configuredPercent[0] > 0 {
		hostPercent = configuredPercent[0]
	}
	layout := hostTableLayout{
		width:       width,
		cpuWidth:    4,
		memoryWidth: 6,
		swapWidth:   6,
	}
	if width >= 58 {
		layout.cpuWidth = 5
		layout.memoryWidth = 7
		layout.swapWidth = 7
		// Reserve only the configured share for HOST and give the reclaimed
		// space to the two dtop-like utilization bars.
		desiredNameWidth := max(12, width*hostPercent/100)
		layout.usageWidth = max(10, min(48, (width-desiredNameWidth-26)/2))
		layout.showUsage = true
	}
	metricWidth := layout.cpuWidth + layout.memoryWidth + layout.swapWidth + 2
	if layout.showUsage {
		metricWidth += 2*layout.usageWidth + 2
	}
	// icon + space + name + space + metrics
	layout.nameWidth = max(1, width-metricWidth-3)
	return layout
}

func renderHostTableHeader(layout hostTableLayout) string {
	parts := []string{
		padRight("HOST", layout.nameWidth),
		padLeft("CPU", layout.cpuWidth),
		padLeft("MEM", layout.memoryWidth),
		padLeft("SWAP", layout.swapWidth),
	}
	if layout.showUsage {
		parts = append(parts,
			padLeft("CPU%", layout.usageWidth),
			padLeft("MEM%", layout.usageWidth),
		)
	}
	return titleStyle.Render(truncate("  "+strings.Join(parts, " "), layout.width))
}

func renderHostMetrics(result probe.Result, ok bool, layout hostTableLayout) string {
	cpuValue, memoryValue, swapValue := "—", "—", "—"
	if ok && result.Status == probe.StatusOnline {
		if result.CPUCount > 0 {
			cpuValue = fmt.Sprintf("%dc", result.CPUCount)
		}
		if result.MemoryTotal > 0 {
			memoryValue = compactBytes(result.MemoryTotal)
		}
		if result.SwapTotal > 0 {
			swapValue = compactBytes(result.SwapTotal)
		}
	}
	parts := []string{
		padLeft(cpuValue, layout.cpuWidth),
		padLeft(memoryValue, layout.memoryWidth),
		padLeft(swapValue, layout.swapWidth),
	}
	if layout.showUsage {
		cpuUsed, cpuValid := 0.0, false
		memoryUsed, memoryValid := 0.0, false
		if ok && result.Status == probe.StatusOnline {
			if result.CPUValid {
				cpuUsed, cpuValid = 100-result.CPUAvailablePct, true
			}
			if result.MemoryTotal > 0 {
				memoryUsed, memoryValid = 100-result.MemoryAvailablePct, true
			}
		}
		parts = append(parts,
			utilizationCell(cpuUsed, cpuValid, layout.usageWidth),
			utilizationCell(memoryUsed, memoryValid, layout.usageWidth),
		)
	}
	return strings.Join(parts, " ")
}

func utilizationCell(used float64, valid bool, width int) string {
	if width <= 0 {
		return ""
	}
	if !valid {
		return mutedStyle.Render(padLeft("…", width))
	}
	used = max(0, min(100, used))
	percentage := fmt.Sprintf("%3.0f%%", used)
	if width <= len(percentage) {
		return utilizationStyle(used).Render(truncate(percentage, width))
	}
	barWidth := max(1, width-len(percentage)-1)
	filled := int(used*float64(barWidth)/100 + 0.5)
	if used > 0 && filled == 0 {
		filled = 1
	}
	filled = min(barWidth, filled)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	return utilizationStyle(used).Render(bar + " " + percentage)
}

func utilizationStyle(used float64) lipgloss.Style {
	style := lipgloss.NewStyle().Foreground(colorGreen)
	if used > 80 {
		return style.Foreground(colorRed)
	}
	if used > 50 {
		return style.Foreground(colorYellow)
	}
	return style
}

func (m Model) renderPreview(width, height int) string {
	if m.embedded != nil {
		return m.renderEmbeddedPreview(width, height)
	}
	if m.embeddedStarting {
		return titleStyle.Render("Starting terminal…") + "\n" +
			mutedStyle.Render("The fleet remains visible while the target gets a PTY.")
	}
	if m.targetStarting {
		return titleStyle.Render("Preparing target session…")
	}
	visible := m.visibleIndices()
	if len(visible) == 0 {
		if m.source > 0 && m.source <= len(m.sources) {
			return renderSourcePreview(m.sources[m.source-1], width, height)
		}
		if group := m.selectedGroup(); group != "" {
			return m.renderGroupPreview(group, width, height)
		}
		return mutedStyle.Render("Select a host")
	}
	index := visible[min(m.selected, len(visible)-1)]
	host := m.hosts[index]
	effective := m.effective[host.ID]
	result, hasResult := m.results[host.ID]

	line := func(label, value string) string {
		return truncate(mutedStyle.Render(label+": ")+session.CleanText(value), width)
	}
	base := []string{
		titleStyle.Render(truncate(session.CleanText(host.DisplayName()), width)),
		line("alias", session.CleanText(host.Alias)),
	}
	if len(host.Tags) > 0 {
		base = append(base, line("tags", strings.Join(host.Tags, ", ")))
	}
	if len(host.Groups) > 0 {
		base = append(base, line("groups", strings.Join(host.Groups, ", ")))
	}
	if host.TargetTransport() == inventory.TransportContainer {
		return m.renderContainerPreview(base, host, result, hasResult, width, height)
	}
	if host.TargetTransport() == inventory.TransportLocal {
		base = append(base,
			"", titleStyle.Render("CONNECTION · LOCAL MACHINE"),
			line("source", host.SourceName),
			line("config", valueOrDash(host.ConfigPath)),
			line("shell", host.Shell+shellArgsText(host.ShellArgs)),
			line("shell origin", valueOrDash(host.ShellOrigin)),
			line("working directory", valueOrDash(host.WorkingDirectory)),
		)
	} else {
		base = append(base,
			"", titleStyle.Render("CONNECTION · LOCAL"),
			line("source", host.SourceName),
			line("config", valueOrDash(host.ConfigPath)),
		)
		if effective.Error != "" {
			base = append(base, line("resolve", lipgloss.NewStyle().Foreground(colorRed).Render(effective.Error)))
		} else {
			base = append(base, line("target", effective.Summary()), line("proxy", valueOrDash(effective.ProxyJump)))
			if len(effective.IdentityFile) > 0 {
				base = append(base, line("identity", effective.IdentityFile[0]))
			}
		}
		if host.CredentialName != "" {
			base = append(base, line("credential", host.CredentialName+" · "+host.CredentialProvider))
		}
	}
	base = append(base, "", titleStyle.Render("SSH CLIENT · LOCAL"))
	if m.client.LocalClient.Error != "" {
		base = append(base, line("error", lipgloss.NewStyle().Foreground(colorRed).Render(m.client.LocalClient.Error)))
	} else {
		base = append(base,
			line("binary", valueOrDash(m.client.LocalClient.Path)),
			line("version", componentText(m.client.LocalClient.Component)),
		)
	}
	if hasResult && result.Status == probe.StatusOnline {
		scope := "REMOTE"
		if host.TargetTransport() == inventory.TransportLocal {
			scope = "LOCAL MACHINE"
		}
		base = appendSSHPreview(base, line, result, scope)
	}

	statusScope := "REMOTE"
	if host.TargetTransport() == inventory.TransportLocal {
		statusScope = "LOCAL MACHINE"
	}
	base = append(base, "", titleStyle.Render("HOST STATUS · "+statusScope))
	if !hasResult {
		base = append(base, mutedStyle.Render("Not probed yet"))
	} else if result.Status == probe.StatusGit {
		base = append(base,
			line("state", string(result.Status)),
			line("latency", result.Latency.Round(time.Millisecond).String()),
			line("authentication", result.GitAuthMethod),
			line("service", valueOrDash(result.GitMessage)),
			mutedStyle.Render("Git endpoint; shell and system metrics are unavailable."),
		)
	} else if result.Status != probe.StatusOnline {
		base = append(base,
			line("state", string(result.Status)),
			line("latency", result.Latency.Round(time.Millisecond).String()),
		)
		if result.ErrorMessage != "" {
			base = append(base, lipgloss.NewStyle().Foreground(colorRed).Render(truncate(result.ErrorMessage, width)))
		}
	} else {
		base = append(base,
			line("state", string(result.Status)),
			line("latency", result.Latency.Round(time.Millisecond).String()),
			line("cpu", coreCountText(result.CPUCount)),
			line("memory", capacityAvailabilityText(result.MemoryTotal, result.MemoryAvail)),
			line("swap", swapText(result.SwapTotal, result.SwapAvail)),
		)
		if result.RootDiskTotal > 0 {
			base = append(base, line("root available", fmt.Sprintf("%s / %s", bytesText(result.RootDiskAvail), bytesText(result.RootDiskTotal))))
		}
		if result.TopProcess.Command != "" {
			base = append(base, line("top process", fmt.Sprintf("%s[%d] %s %.1f%%", result.TopProcess.Command, result.TopProcess.PID, result.TopProcess.State, result.TopProcess.CPU)))
		}
		base = appendSystemPreview(base, line, result)
	}
	if host.TargetTransport() == inventory.TransportSSH {
		base = m.appendHostKeyPreview(base, line, index, result)
	}
	if groupResult, ok := m.groupResults[host.ID]; ok {
		state := "ok"
		if groupResult.Err != "" {
			state = "error · " + groupResult.Err
		}
		base = append(base, "", titleStyle.Render("LAST GROUP COMMAND · LOCAL"),
			line("preset", groupResult.Preset), line("state", state), line("duration", groupResult.Duration.Round(time.Millisecond).String()))
		for _, output := range groupResult.Lines {
			base = append(base, mutedStyle.Render("│ ")+truncate(session.CleanText(output), max(1, width-2)))
		}
	}

	tail := m.sessionTail[host.ID]
	if result.Status == probe.StatusHostKey {
		// The structured repair block above is clearer and safer than repeating
		// OpenSSH's full warning banner (rows of @ characters) as session output.
		tail = nil
	}
	if len(tail) == 0 {
		return strings.Join(base[:min(len(base), height)], "\n")
	}
	tailLimit := min(len(tail), max(3, height/3))
	baseLimit := max(1, height-tailLimit-2)
	base = base[:min(len(base), baseLimit)]
	lines := append(base, "", titleStyle.Render("LAST SESSION · "+statusScope))
	for _, output := range tail[len(tail)-tailLimit:] {
		lines = append(lines, mutedStyle.Render("│ ")+truncate(output, max(1, width-2)))
	}
	return strings.Join(lines[:min(len(lines), height)], "\n")
}

func (m Model) renderGroupPreview(name string, width, height int) string {
	line := func(label, value string) string {
		return truncate(mutedStyle.Render(label+": ")+session.CleanText(value), width)
	}
	count := 0
	for _, host := range m.hosts {
		if contains(host.Groups, name) {
			count++
		}
	}
	lines := []string{
		titleStyle.Render(truncate(name, width)),
		"",
		titleStyle.Render("PRIVATE GROUP"),
		line("resolved hosts", fmt.Sprintf("%d", count)),
		line("source mutation", "never"),
	}
	if group, ok := m.groupDefinition(name); ok {
		lines = append(lines,
			line("members", fmt.Sprintf("%d", len(group.Members))),
			line("patterns", fmt.Sprintf("%d", len(group.Match))),
		)
		for _, member := range group.Members {
			lines = append(lines, line("member", member))
		}
		for _, pattern := range group.Match {
			lines = append(lines, line("match", pattern))
		}
	}
	lines = append(lines,
		"",
		mutedStyle.Render("m on host: membership"),
		mutedStyle.Render("R rename · D delete · e edit"),
		mutedStyle.Render("x command preset"),
	)
	return strings.Join(lines[:min(len(lines), height)], "\n")
}

func renderSourcePreview(source inventory.SourceSummary, width, height int) string {
	line := func(label, value string) string {
		return truncate(mutedStyle.Render(label+": ")+session.CleanText(value), width)
	}
	state := source.State
	if state == "" {
		state = inventory.SourceStateLoaded
	}
	lines := []string{
		titleStyle.Render(truncate(session.CleanText(source.Name), width)),
		"",
		titleStyle.Render("SOURCE STATUS"),
		line("state", string(state)),
		line("hosts", fmt.Sprintf("%d", source.Hosts)),
		line("origin", valueOrDash(source.Path)),
	}
	if source.Detail != "" {
		lines = append(lines, line("details", source.Detail))
	}
	if source.Err != nil {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorRed).Render(
			truncate(session.CleanText(source.Err.Error()), width),
		))
	}
	return strings.Join(lines[:min(len(lines), height)], "\n")
}

func appendSSHPreview(base []string, line func(string, string) string, result probe.Result, scope string) []string {
	base = append(base,
		"",
		titleStyle.Render("SSH SOFTWARE · "+scope),
		line("client", componentText(result.SSHClient)),
		line("server", componentText(result.SSHServer)),
	)
	if result.SSHServiceUnit != "" {
		base = append(base, line("service unit", result.SSHServiceUnit))
	}
	if result.SSHSocketState != "" && result.SSHSocketState != "not-installed" {
		base = append(base, line("socket", result.SSHSocketState))
	}
	base = append(base, line("agent", componentText(result.SSHAgent)))
	if result.SSHTools != "" {
		base = append(base, line("tools", result.SSHTools))
	}
	base = append(base, line("openssl", componentText(result.OpenSSL)))
	return base
}

func (m Model) renderContainerPreview(base []string, host inventory.Host, result probe.Result, hasResult bool, width, height int) string {
	line := func(label, value string) string {
		return truncate(mutedStyle.Render(label+": ")+session.CleanText(value), width)
	}
	base = append(base,
		"", titleStyle.Render("CONNECTION · LOCAL CONTAINER"),
		line("source", host.SourceName),
		line("runtime", host.ContainerRuntime),
		line("context", valueOrDash(host.ContainerContext)),
		line("endpoint", valueOrDash(host.ContainerEndpoint)),
		line("container id", host.ContainerID),
		line("image", valueOrDash(host.ContainerImage)),
		line("platform", valueOrDash(host.ContainerPlatform)),
		line("state", valueOrDash(host.ContainerState)),
		line("health", valueOrDash(host.ContainerHealth)),
		line("status", valueOrDash(host.ContainerStatus)),
		line("entrypoint", valueOrDash(host.ContainerEntrypoint)),
		line("command", valueOrDash(host.ContainerCommand)),
		line("restart", valueOrDash(host.ContainerRestart)),
		line("mounts", valueOrDash(host.ContainerMounts)),
		line("networks", valueOrDash(host.ContainerNetworks)),
		line("ports", valueOrDash(host.ContainerPorts)),
		line("shell policy", valueOrDash(host.ContainerShellPolicy)),
		line("effective shell", valueOrDash(host.ContainerShell)),
		line("shell priority", strings.Join(host.ContainerShells, ", ")),
		"", titleStyle.Render("HOST STATUS · LOCAL CONTAINER"),
	)
	if host.ContainerDiscoveryState == string(inventory.SourceStateStale) {
		base = append(base, lipgloss.NewStyle().Foreground(colorYellow).Render(truncate("stale: "+host.ContainerDiscoveryError, width)))
	}
	if host.ContainerInspectError != "" {
		base = append(base, lipgloss.NewStyle().Foreground(colorYellow).Render(truncate("inspect: "+host.ContainerInspectError, width)))
	}
	if !hasResult {
		base = append(base, mutedStyle.Render("Waiting for runtime refresh"))
	} else {
		base = append(base, line("state", string(result.Status)))
		if result.ErrorMessage != "" {
			base = append(base, lipgloss.NewStyle().Foreground(colorRed).Render(truncate(result.ErrorMessage, width)))
		}
	}
	if tail := m.sessionTail[host.ID]; len(tail) > 0 {
		tailLimit := min(len(tail), max(3, height/3))
		base = base[:min(len(base), max(1, height-tailLimit-2))]
		base = append(base, "", titleStyle.Render("LAST SESSION · LOCAL CONTAINER"))
		for _, output := range tail[len(tail)-tailLimit:] {
			base = append(base, mutedStyle.Render("│ ")+truncate(session.CleanText(output), max(1, width-2)))
		}
	}
	return strings.Join(base[:min(len(base), height)], "\n")
}

func shellArgsText(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return " · argv: " + strings.Join(args, " ")
}

func (m Model) renderEmbeddedPreview(width, height int) string {
	if m.embedded == nil {
		return ""
	}
	alias := "target"
	if m.embeddedIndex >= 0 && m.embeddedIndex < len(m.hosts) {
		alias = m.hosts[m.embeddedIndex].DisplayName()
	}
	state := "Ctrl+] close"
	if m.embeddedClosing {
		state = "closing…"
	}
	banner := selectedDim.Render(padRight(truncate(" TERMINAL · "+alias+" · "+state+" ", width), width))
	terminalLines := strings.Split(strings.TrimSuffix(m.embedded.Terminal.Render(), "\n"), "\n")
	lines := append([]string{banner}, terminalLines...)
	return fitLines(lines, width, height)
}

func (m Model) overlayActionMenu(base string, width, height int) string {
	actions := m.availableHostActions(m.actionHostIndex)
	if len(actions) == 0 || width < 20 || height < 6 {
		return base
	}
	menuWidth := min(56, max(38, width/3))
	menuWidth = min(menuWidth, max(20, width-4))
	innerWidth := max(1, menuWidth-4)
	hostName := "host"
	if m.actionHostIndex >= 0 && m.actionHostIndex < len(m.hosts) {
		hostName = session.CleanText(m.hosts[m.actionHostIndex].DisplayName())
	}
	lines := []string{titleStyle.Render(truncate("ACTIONS · "+hostName, innerWidth)), ""}
	for index, action := range actions {
		prefix := "  "
		if index == m.actionSelected {
			prefix = "› "
		}
		line := padRight(truncate(prefix+session.CleanText(action.label), innerWidth), innerWidth)
		if index == m.actionSelected {
			line = selected.Render(ansi.Strip(line))
		}
		lines = append(lines, line)
	}
	selectedAction := actions[min(m.actionSelected, len(actions)-1)]
	lines = append(lines, "", mutedStyle.Render(truncate(session.CleanText(selectedAction.description), innerWidth)))
	popup := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBlue).
		Padding(0, 1).
		Width(menuWidth - 2).
		Render(strings.Join(lines, "\n"))
	popupWidth := ansi.StringWidth(strings.Split(popup, "\n")[0])
	popupHeight := len(strings.Split(popup, "\n"))
	x := max(0, (width-popupWidth)/2)
	y := max(0, (height-popupHeight)/2)
	return overlayBlock(base, popup, x, y, width, height)
}

func (m Model) overlayGroupCommandMenu(base string, width, height int) string {
	if len(m.commands) == 0 || width < 30 || height < 8 {
		return base
	}
	menuWidth := min(72, max(46, width/2))
	innerWidth := menuWidth - 4
	group := m.selectedGroup()
	lines := []string{titleStyle.Render(truncate("GROUP COMMAND · "+group, innerWidth)), ""}
	if !m.groupCommandConfirm {
		for index, command := range m.commands {
			prefix := "  "
			if index == m.groupCommandChoice {
				prefix = "› "
			}
			line := padRight(truncate(prefix+command.Name, innerWidth), innerWidth)
			if index == m.groupCommandChoice {
				line = selected.Render(ansi.Strip(line))
			}
			lines = append(lines, line)
		}
		lines = append(lines, "", mutedStyle.Render("Enter: plan · Esc: cancel"))
	} else {
		command := m.commands[min(m.groupCommandChoice, len(m.commands)-1)]
		lines = append(lines,
			"preset: "+truncate(command.Name, max(1, innerWidth-8)),
			"argv: "+truncate(strings.Join(command.Argv, " "), max(1, innerWidth-6)),
			fmt.Sprintf("targets: %d", len(m.groupHostIndices())),
			"timeout: "+command.Timeout.Duration.String(),
			"",
			lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render("Enter again to run on this immutable target list"),
			mutedStyle.Render("Esc: back"),
		)
	}
	popup := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorPurple).Padding(0, 1).Width(menuWidth - 2).Render(strings.Join(lines, "\n"))
	return centeredOverlay(base, popup, width, height)
}

func (m Model) overlayHealth(base string, width, height int) string {
	menuWidth := min(88, max(54, width*2/3))
	innerWidth := menuWidth - 4
	lines := []string{titleStyle.Render("APPLICATION HEALTHCHECK"), ""}
	for _, result := range m.health {
		value := string(result.Origin) + " · " + result.Path
		if result.Error != "" {
			value = "missing · " + result.Error
		}
		lines = append(lines, padRight(result.Name, 9)+truncate(session.CleanText(value), max(1, innerWidth-9)))
	}
	editor := "missing · " + m.editorHealth.Error
	if m.editorHealth.Error == "" {
		editor = m.editorHealth.Name + " · " + string(m.editorHealth.Origin) + " · " + m.editorHealth.Path
	}
	hostsPercent := max(0, 100-m.sourcesWidthPct-m.previewWidthPct)
	lines = append(lines,
		"", "editor   "+truncate(session.CleanText(editor), max(1, innerWidth-9)),
		"", titleStyle.Render("LAYOUT · [app.ui]"),
		fmt.Sprintf("config   SOURCES %d%% · HOSTS %d%% · PREVIEW %d%%", m.sourcesWidthPct, hostsPercent, m.previewWidthPct),
		fmt.Sprintf("nested   HOST name %d%% · metrics and bars %d%%", m.hostColumnPct, max(0, 100-m.hostColumnPct)),
	)
	if width >= 100 {
		left, middle, right := m.widePaneWidths(width)
		table := newHostTableLayout(middle, m.hostColumnPct)
		lines = append(lines,
			fmt.Sprintf("current  SOURCES %dc · HOSTS %dc · PREVIEW %dc", left, middle, right),
			fmt.Sprintf("table    HOST name %dc · CPU%%/MEM%% bars %dc each", table.nameWidth, table.usageWidth),
		)
	}
	lines = append(lines,
		mutedStyle.Render("c: edit config · restart SSH Fleet Console to apply"),
		"", mutedStyle.Render("? / Esc: close"),
	)
	popup := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBlue).Padding(0, 1).Width(menuWidth - 2).Render(strings.Join(lines, "\n"))
	return centeredOverlay(base, popup, width, height)
}

func centeredOverlay(base, popup string, width, height int) string {
	popupLines := strings.Split(popup, "\n")
	popupWidth := 0
	for _, line := range popupLines {
		popupWidth = max(popupWidth, ansi.StringWidth(line))
	}
	return overlayBlock(base, popup, max(0, (width-popupWidth)/2), max(0, (height-len(popupLines))/2), width, height)
}

func overlayBlock(base, popup string, x, y, width, height int) string {
	baseLines := strings.Split(base, "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, "")
	}
	popupLines := strings.Split(popup, "\n")
	popupWidth := 0
	for _, line := range popupLines {
		popupWidth = max(popupWidth, ansi.StringWidth(line))
	}
	for offset, popupLine := range popupLines {
		row := y + offset
		if row < 0 || row >= height {
			continue
		}
		baseLine := padRight(truncate(baseLines[row], width), width)
		left := ansi.Cut(baseLine, 0, min(x, width))
		rightStart := min(width, x+popupWidth)
		right := ansi.Cut(baseLine, rightStart, width)
		baseLines[row] = padRight(truncate(left+padRight(popupLine, popupWidth)+right, width), width)
	}
	return strings.Join(baseLines[:height], "\n")
}

func (m Model) renderFooter(width int) string {
	left := " Tab/h/l pane  j/k move  / filter  e host  c config  ? health  Enter actions  q quit "
	if m.embedded != nil {
		left = " PREVIEW TERMINAL  all keys go to target  Ctrl+] close "
	} else if m.embeddedStarting {
		left = " PREVIEW TERMINAL  starting…  Ctrl+C quit "
	} else if m.targetStarting {
		left = " TARGET SESSION  preparing…  Ctrl+C quit "
	} else if m.workspaceStarting {
		left = " BUNDLED WORKSPACE  validating and uploading…  Ctrl+C quit "
	} else if m.actionMenu {
		left = " ACTIONS  ↑/↓ or j/k move  Enter run  Esc cancel "
	} else if m.groupCommandMenu {
		left = " GROUP COMMAND  j/k move  Enter plan/run  Esc back "
	} else if m.groupDialog.mode != groupDialogNone {
		left = " GROUPS  type or j/k move  Enter apply  Esc cancel "
	} else if m.hostKeyPlan != nil {
		left = " HOST KEY CHANGE  Shift+K backup+remove  Esc cancel "
	} else if m.hostKeyBusy {
		left = " HOST KEY CHANGE  inspecting… "
	} else if index := m.selectedHostIndex(); index >= 0 {
		hostID := m.hosts[index].ID
		if m.hostKeyPrompt[hostID] {
			left = " Enter actions  backup saved  q quit "
		} else if result, ok := m.results[hostID]; ok && result.Status == probe.StatusHostKey {
			left = " Shift+K inspect host-key repair  Enter actions  q quit "
		} else if ok && result.Status == probe.StatusGit {
			left = " j/k move  / filter  e host  c config  r refresh  Enter actions  q quit "
		}
	}
	if m.selectedGroup() != "" && !m.groupCommandMenu && !m.filtering && m.groupDialog.mode == groupDialogNone {
		left = " GROUP " + m.selectedGroup() + "  x command  R rename  D delete  e edit  Enter hosts "
	} else if m.focus == focusSources && m.groupDialog.mode == groupDialogNone {
		left = " SOURCES  j/k move  n new group  Enter hosts  ? health  q quit "
	}
	if m.filtering {
		left = " filter: " + m.filter + "█  Enter apply  Esc close "
	} else if m.filter != "" {
		left = " /" + m.filter + "  Esc clear  Tab pane  Enter actions  q quit "
	}
	right := ""
	if m.active > 0 {
		right = fmt.Sprintf("polling %d/%d ", m.active, m.active+len(m.queue))
	} else if !m.lastRefresh.IsZero() {
		right = "updated " + ageText(m.lastRefresh) + " "
	}
	if m.message != "" {
		right = truncate(session.CleanText(m.message), max(10, width/2)) + " "
	}
	space := max(1, width-ansi.StringWidth(left)-ansi.StringWidth(right))
	return statusStyle.Render(padRight(truncate(left+strings.Repeat(" ", space)+right, width), width))
}

func (m Model) appendHostKeyPreview(base []string, line func(string, string) string, index int, result probe.Result) []string {
	hostID := m.hosts[index].ID
	backup := m.hostKeyBackup[hostID]
	if result.Status != probe.StatusHostKey && backup == "" {
		return base
	}

	title := lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render("HOST KEY CHANGE")
	if backup != "" {
		title = lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render("HOST KEY REPAIR")
	}
	base = append(base, "", title)
	if result.PresentedHostKey != "" {
		base = append(base, line("presented (untrusted)", result.PresentedHostKey))
	}
	if result.KnownHostsFile != "" {
		location := result.KnownHostsFile
		if result.KnownHostsLine > 0 {
			location += fmt.Sprintf(":%d", result.KnownHostsLine)
		}
		base = append(base, line("offending", location))
	}
	if backup != "" {
		base = append(base, line("backup", backup))
		if m.hostKeyPrompt[hostID] {
			base = append(base, lipgloss.NewStyle().Foreground(colorYellow).Render("Old entry removed. Enter forces a host-key prompt."))
		} else {
			base = append(base, lipgloss.NewStyle().Foreground(colorGreen).Render("Replacement key accepted; backup retained."))
		}
		return base
	}
	if m.hostKeyPlan != nil && m.hostKeyIndex == index {
		base = append(base, line("lookup", m.hostKeyPlan.Lookup))
		for storedIndex, fingerprint := range m.hostKeyPlan.StoredFingerprints {
			label := "stored"
			if storedIndex > 0 {
				label = "stored " + fmt.Sprint(storedIndex+1)
			}
			base = append(base, line(label, fingerprint))
		}
		base = append(base,
			lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render("VERIFY PRESENTED FINGERPRINT OUT OF BAND"),
			mutedStyle.Render("Shift+K: backup + remove old entries · Esc: cancel"),
		)
		return base
	}
	base = append(base, mutedStyle.Render("Shift+K: inspect backup-first repair; no key is auto-trusted"))
	return base
}

func (m Model) statusIcon(host inventory.Host, result probe.Result, ok bool) string {
	if m.polling[host.ID] {
		return lipgloss.NewStyle().Foreground(colorBlue).Render("◌")
	}
	if !host.Probe || !ok {
		return mutedStyle.Render("○")
	}
	style := lipgloss.NewStyle()
	switch result.Status {
	case probe.StatusOnline, probe.StatusGit:
		style = style.Foreground(colorGreen)
	case probe.StatusAuth:
		style = style.Foreground(colorPurple)
	case probe.StatusHostKey:
		style = style.Foreground(colorYellow)
	default:
		style = style.Foreground(colorRed)
	}
	return style.Render("●")
}

func coreCountText(count int) string {
	if count <= 0 {
		return "—"
	}
	if count == 1 {
		return "1 core"
	}
	return fmt.Sprintf("%d cores", count)
}

func capacityAvailabilityText(total, available uint64) string {
	if total == 0 {
		return "—"
	}
	return fmt.Sprintf("%s total · %s available", bytesText(total), bytesText(available))
}

func swapText(total, available uint64) string {
	if total == 0 {
		return "none"
	}
	return capacityAvailabilityText(total, available)
}

func appendSystemPreview(base []string, line func(string, string) string, result probe.Result) []string {
	base = append(base, "", titleStyle.Render("SYSTEM · REMOTE"))
	if result.OSName != "" {
		base = append(base, line("os", result.OSName))
	}
	if result.Kernel != "" || result.Architecture != "" {
		base = append(base, line("kernel", strings.TrimSpace(result.Kernel+" "+result.Architecture)))
	}
	if result.Init != "" {
		base = append(base, line("init", result.Init))
	}
	if result.Virtualization != "" {
		base = append(base, line("virtualization", result.Virtualization))
	}
	if result.Systemd.State != "" || result.Systemd.Version != "" {
		text := componentText(result.Systemd)
		if result.Init == "systemd" {
			text += fmt.Sprintf(" · failed %d", result.SystemdFailedUnits)
		}
		base = append(base, line("systemd", text))
	}
	base = append(base,
		line("docker", componentText(result.Docker)),
		line("containerd", componentText(result.Containerd)),
		line("podman", componentText(result.Podman)),
		line("kubelet", componentText(result.Kubelet)),
	)
	return base
}

func componentText(component probe.Component) string {
	parts := make([]string, 0, 2)
	if component.State != "" {
		parts = append(parts, component.State)
	}
	if component.Version != "" {
		parts = append(parts, component.Version)
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, " · ")
}

func processText(host inventory.Host, result probe.Result, ok bool) string {
	if host.TargetTransport() == inventory.TransportContainer {
		parts := []string{valueOrDash(host.ContainerImage), valueOrDash(host.ContainerStatus)}
		if host.ContainerPorts != "" {
			parts = append(parts, host.ContainerPorts)
		}
		return strings.Join(parts, " · ")
	}
	if !ok {
		return "waiting for probe"
	}
	if result.Status == probe.StatusGit {
		return "git: access via " + result.GitAuthMethod + " · " + ageText(result.CheckedAt)
	}
	if result.Status != probe.StatusOnline {
		if result.ErrorMessage != "" {
			return result.ErrorMessage
		}
		return string(result.Status)
	}
	if result.TopProcess.Command == "" {
		return "online · " + ageText(result.CheckedAt)
	}
	return fmt.Sprintf("top: %s[%d] %s %.1f%% · %s", result.TopProcess.Command, result.TopProcess.PID, result.TopProcess.State, result.TopProcess.CPU, ageText(result.CheckedAt))
}

func verticalDivider(height int) string {
	lines := make([]string, height)
	for i := range lines {
		lines[i] = divider.Render("│")
	}
	return strings.Join(lines, "\n")
}

func fitLines(lines []string, width, height int) string {
	if height <= 0 {
		return ""
	}
	out := make([]string, height)
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out[i] = padRight(truncate(lines[i], width), width)
		} else {
			out[i] = strings.Repeat(" ", width)
		}
	}
	return strings.Join(out, "\n")
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

func padRight(s string, width int) string {
	missing := width - ansi.StringWidth(s)
	if missing <= 0 {
		return s
	}
	return s + strings.Repeat(" ", missing)
}

func padLeft(s string, width int) string {
	missing := width - ansi.StringWidth(s)
	if missing <= 0 {
		return s
	}
	return strings.Repeat(" ", missing) + s
}

func valueOrDash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

func bytesText(value uint64) string {
	const gib = 1024 * 1024 * 1024
	const mib = 1024 * 1024
	if value >= gib {
		return fmt.Sprintf("%.1f GiB", float64(value)/gib)
	}
	return fmt.Sprintf("%.0f MiB", float64(value)/mib)
}

func compactBytes(value uint64) string {
	if value == 0 {
		return "--"
	}
	const gib = 1024 * 1024 * 1024
	const mib = 1024 * 1024
	if value >= gib {
		return fmt.Sprintf("%.1fG", float64(value)/gib)
	}
	return fmt.Sprintf("%.0fM", float64(value)/mib)
}

func durationText(value time.Duration) string {
	if value <= 0 {
		return "—"
	}
	days := int(value.Hours()) / 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, int(value.Hours())%24)
	}
	return value.Round(time.Minute).String()
}

func ageText(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	age := time.Since(t)
	if age < time.Second {
		return "now"
	}
	if age < time.Minute {
		return fmt.Sprintf("%ds ago", int(age.Seconds()))
	}
	return fmt.Sprintf("%dm ago", int(age.Minutes()))
}
