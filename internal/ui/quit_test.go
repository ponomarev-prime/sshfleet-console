package ui

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ponomarev-prime/sshfleet-console/internal/inventory"
	"github.com/ponomarev-prime/sshfleet-console/internal/openssh"
)

func quitKey(text string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: rune(text[0]), Text: text})
}

func TestQuitAlwaysRequiresConfirmation(t *testing.T) {
	for _, confirm := range []tea.KeyPressMsg{quitKey("q"), quitKey("y"), tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}), tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})} {
		t.Run(confirm.String(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m := New(nil, nil, openssh.Client{}, time.Minute, 1)
			m.startupCancel = cancel
			m.startupLoading = true
			m.width, m.height = 80, 24
			next, cmd := m.Update(quitKey("q"))
			m = next.(Model)
			if cmd != nil || !m.quitConfirm || ctx.Err() != nil {
				t.Fatal("first q quit or cancelled work")
			}
			if view := ansi.Strip(m.View().Content); !strings.Contains(view, "QUIT SSH FLEET?") || !strings.Contains(view, "Esc / n: cancel") {
				t.Fatal(view)
			}
			next, cmd = m.Update(confirm)
			m = next.(Model)
			if cmd == nil || m.quitConfirm || ctx.Err() == nil {
				t.Fatal("confirmed exit did not cancel background work")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("confirmed exit did not quit")
			}
		})
	}
}

func TestQuitCancelNavigationRepeatAndPaste(t *testing.T) {
	for _, cancel := range []tea.Msg{tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}), quitKey("n"), quitKey("j"), tea.PasteMsg{Content: "qq"}} {
		m := New([]inventory.Host{{ID: "a", Alias: "a"}, {ID: "b", Alias: "b"}}, nil, openssh.Client{}, time.Minute, 1)
		next, _ := m.Update(quitKey("q"))
		m = next.(Model)
		next, cmd := m.Update(cancel)
		m = next.(Model)
		if m.quitConfirm {
			t.Fatalf("cancel left exit armed: %T", cancel)
		}
		if key, ok := cancel.(tea.KeyPressMsg); ok && key.String() == "j" && m.selected != 1 {
			t.Fatal("navigation was swallowed")
		}
		if key, ok := cancel.(tea.KeyPressMsg); !ok || key.String() != "j" {
			if cmd != nil {
				t.Fatal("cancel scheduled a command")
			}
		}
		next, cmd = m.Update(quitKey("q"))
		m = next.(Model)
		if cmd != nil || !m.quitConfirm {
			t.Fatal("a later q bypassed a fresh confirmation")
		}
	}
	m := New(nil, nil, openssh.Client{}, time.Minute, 1)
	next, _ := m.Update(quitKey("q"))
	m = next.(Model)
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Text: "q", IsRepeat: true}))
	m = next.(Model)
	if cmd != nil || !m.quitConfirm {
		t.Fatal("key repeat confirmed exit")
	}
}

func TestQInSearchAndActionMenuDoesNotRequestExit(t *testing.T) {
	m := New([]inventory.Host{{ID: "a", Alias: "a"}}, nil, openssh.Client{}, time.Minute, 1)
	m.filtering = true
	next, _ := m.Update(quitKey("q"))
	m = next.(Model)
	if m.filter != "q" || m.quitConfirm {
		t.Fatal("search q requested exit")
	}
	m.filtering, m.actionMenu = false, true
	next, cmd := m.Update(quitKey("q"))
	m = next.(Model)
	if cmd != nil || m.actionMenu || m.quitConfirm {
		t.Fatal("menu q requested exit")
	}
}

func TestQuitWithLiveTabWaitsForCleanup(t *testing.T) {
	m := New(nil, nil, openssh.Client{}, time.Minute, 1)
	m.width, m.height = 100, 24
	next, start := m.openTerminalTab(exec.Command("sh", "-c", "sleep 30"), -1, "fixture", false)
	m = next.(Model)
	started := start().(terminalTabStartedMsg)
	if started.err != nil {
		t.Fatal(started.err)
	}
	defer started.session.Close()
	next, _ = m.Update(started)
	m = next.(Model)
	m.activeTab = 0
	next, cmd := m.Update(quitKey("q"))
	m = next.(Model)
	if cmd != nil || m.tabs[0].state != terminalTabRunning {
		t.Fatal("first q closed live session")
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "terminate 1 live terminal session") {
		t.Fatal(view)
	}
	next, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	m = next.(Model)
	if cmd != nil || m.quitConfirm || m.tabs[0].state != terminalTabRunning {
		t.Fatal("cancelled exit affected a live session")
	}
	next, _ = m.Update(quitKey("q"))
	m = next.(Model)
	next, cmd = m.Update(quitKey("q"))
	m = next.(Model)
	if cmd == nil || !m.quitAfterTabsClose || m.tabs[0].state != terminalTabClosing {
		t.Fatal("confirmed exit skipped cleanup")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		if len(batch) != 1 {
			t.Fatal(len(batch))
		}
		msg = batch[0]()
	}
	next, quit := m.Update(msg)
	m = next.(Model)
	if len(m.tabs) != 0 || quit == nil {
		t.Fatal("cleanup did not finish before exit")
	}
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatal("cleanup did not return quit")
	}
}

func TestCtrlCAlsoRequestsConfirmation(t *testing.T) {
	m := New(nil, nil, openssh.Client{}, time.Minute, 1)
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if cmd != nil || !next.(Model).quitConfirm {
		t.Fatal("Ctrl+C bypassed confirmation")
	}
}
