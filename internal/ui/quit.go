package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) overlayQuitPrompt(base string, width, height int) string {
	live := 0
	for _, tab := range m.tabs {
		if tab.session != nil || tab.state == terminalTabStarting {
			live++
		}
	}
	text := "QUIT SSH FLEET?"
	if live > 0 {
		text += fmt.Sprintf("\nThis will terminate %d live terminal session(s).", live)
	}
	text += "\nq / Enter / y: confirm\nEsc / n: cancel"
	if width < 40 || height < 10 {
		return centeredOverlay(base, ansi.Hardwrap(text, max(1, width), true), width, height)
	}
	inner := min(58, width-6)
	lines := strings.Split(ansi.Hardwrap(text, inner, true), "\n")
	popup := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).Width(inner).Render(strings.Join(lines, "\n"))
	return centeredOverlay(base, popup, width, height)
}
