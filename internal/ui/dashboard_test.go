package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func press(m *dashboard, key rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
	return cmd
}
func special(m *dashboard, key rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: key})
	return cmd
}
func fixtureDashboard() *dashboard {
	m := newDashboard(DashboardOptions{Repository: "garden", Load: func(Page) (DashboardSnapshot, error) { return DashboardSnapshot{}, nil }, ValidateBranch: func(s string) error {
		if strings.Contains(s, " ") {
			return errors.New("invalid branch")
		}
		return nil
	}})
	m.snapshot = DashboardSnapshot{Rows: []DashboardRow{
		{ID: "a", Label: "feature/庭", Status: "dirty", Details: "Current branch\nPath: /trees/one", Action: Action{Kind: ResumeBranch, Target: "a"}},
		{ID: "b", Label: "feature/quiet", Status: "available", Details: "Last used: feature/old", Action: Action{Kind: ResumeBranch, Target: "b"}},
	}}
	return m
}
func TestDashboardKeyboard(t *testing.T) {
	m := fixtureDashboard()
	press(m, 'j')
	if m.selected != 1 {
		t.Fatal("j did not move")
	}
	press(m, '/')
	press(m, 'q')
	if m.action.Kind != NoAction || len(m.rows()) != 1 {
		t.Fatal("search q quit or did not filter")
	}
	special(m, tea.KeyEscape)
	if m.searching || len(m.rows()) != 2 {
		t.Fatal("escape did not clear")
	}
	press(m, 'j')
	special(m, tea.KeyEnter)
	if m.action.Kind != ResumeBranch || m.action.Target != "b" {
		t.Fatalf("wrong action: %+v", m.action)
	}
}
func TestDashboardValidationPreservesText(t *testing.T) {
	m := fixtureDashboard()
	press(m, 'n')
	m.input.SetValue("bad branch")
	cmd := special(m, tea.KeyEnter)
	m.Update(cmd())
	if m.err == "" || m.input.Value() != "bad branch" || m.action.Kind != NoAction {
		t.Fatal("validation discarded text or submitted")
	}
	m.input.SetValue("feature/new")
	cmd = special(m, tea.KeyEnter)
	m.Update(cmd())
	if m.action.Kind != CreateBranch || m.action.Target != "feature/new" {
		t.Fatal("valid branch not submitted")
	}
}
func TestDashboardStaleLoadsAndSelection(t *testing.T) {
	m := fixtureDashboard()
	press(m, 'j')
	m.reload()
	g := m.generation
	m.Update(snapshotMsg{generation: g - 1, snapshot: DashboardSnapshot{Summary: "stale"}})
	if !m.loading || m.snapshot.Summary == "stale" {
		t.Fatal("stale result applied")
	}
	m.Update(snapshotMsg{generation: g, snapshot: m.snapshot})
	if m.selected != 1 || m.loading {
		t.Fatal("selection lost")
	}
	m.navigate(TreesPage)
	m.Update(snapshotMsg{generation: g, snapshot: DashboardSnapshot{Summary: "wrong page"}})
	if m.snapshot.Summary == "wrong page" {
		t.Fatal("old page load applied")
	}
}
func TestDashboardCleanupDefaultsToCancel(t *testing.T) {
	m := fixtureDashboard()
	m.page = CleanupPage
	m.snapshot.CandidatePaths = []string{"/only/displayed"}
	special(m, tea.KeyEnter)
	if m.action.Kind != NoAction || m.page != ResultPage || !strings.Contains(m.options.Result, "Canceled") {
		t.Fatal("default removed trees")
	}
	m.page = CleanupPage
	special(m, tea.KeyTab)
	special(m, tea.KeyEnter)
	if m.action.Kind != RemoveTrees || len(m.action.Paths) != 1 || m.action.Paths[0] != "/only/displayed" {
		t.Fatal("candidate restriction lost")
	}
}
func TestDashboardLayout(t *testing.T) {
	for _, width := range []int{32, 60, 99, 100, 140} {
		for _, dark := range []bool{false, true} {
			for _, mono := range []bool{false, true} {
				m := fixtureDashboard()
				m.width = width
				m.height = 20
				m.dark = dark
				m.monochrome = mono
				m.snapshot.Rows[0].Label = strings.Repeat("庭é", 100)
				m.snapshot.Rows[0].Details = strings.Repeat("Long details about 庭 work\n", 60)
				view := m.View().Content
				if lipgloss.Height(view) > m.height {
					t.Fatalf("height overflow at %d: %d", width, lipgloss.Height(view))
				}
				for _, line := range strings.Split(view, "\n") {
					if lipgloss.Width(line) > width {
						t.Fatalf("width overflow at %d: %d", width, lipgloss.Width(line))
					}
				}
				if !strings.Contains(view, "[dirty]") {
					t.Fatal("long branch hid its status badge")
				}
				if !strings.Contains(view, "j/k") || !strings.Contains(view, ">") {
					t.Fatal("footer or selection absent")
				}
				if mono && strings.Contains(view, "\x1b") {
					t.Fatal("NO_COLOR emitted style escapes")
				}
			}
		}
	}
}
func TestDashboardEmptyErrorAndNoMatches(t *testing.T) {
	m := fixtureDashboard()
	m.snapshot.Rows = nil
	if !strings.Contains(m.View().Content, "No available branches") {
		t.Fatal("empty state missing")
	}
	m = fixtureDashboard()
	press(m, '/')
	m.input.SetValue("not found")
	if !strings.Contains(m.View().Content, "No matches") {
		t.Fatal("no-match state missing")
	}
	m.Update(snapshotMsg{generation: m.generation, err: errors.New("cannot read pool")})
	if !strings.Contains(m.View().Content, "cannot read pool") {
		t.Fatal("error missing")
	}
}

func TestDashboardPastedBranch(t *testing.T) {
	m := fixtureDashboard()
	press(m, 'n')
	m.Update(tea.PasteMsg{Content: "feature/pasted"})
	if m.input.Value() != "feature/pasted" {
		t.Fatal("paste did not reach branch input")
	}
}

func TestDashboardCleanupRetainsHomeSelection(t *testing.T) {
	m := fixtureDashboard()
	m.options.Selection = m.selection
	press(m, 'j')
	m.navigate(CleanupPage)
	m.finish(Action{Kind: RemoveTrees})
	next := newDashboard(m.options)
	cmd := next.Init()
	if cmd == nil {
		t.Fatal("home did not load")
	}
	rows := fixtureDashboard().snapshot
	next.Update(snapshotMsg{generation: next.generation, snapshot: rows})
	if next.selected != 1 {
		t.Fatal("cleanup lost branch selection")
	}
}

func TestDashboardLongListAndJJ(t *testing.T) {
	m := fixtureDashboard()
	m.width = 60
	m.height = 16
	for i := 0; i < 50; i++ {
		m.snapshot.Rows = append(m.snapshot.Rows, DashboardRow{ID: "extra", Label: "other work", Status: "available"})
	}
	m.snapshot.Rows[len(m.snapshot.Rows)-1].Label = "last branch"
	for range m.snapshot.Rows {
		press(m, 'j')
	}
	if !strings.Contains(m.View().Content, "last branch") {
		t.Fatal("selection scrolled out of view")
	}
	m = fixtureDashboard()
	m.options.JJ = true
	press(m, 'n')
	if m.creating || m.action.Kind != StartTree {
		t.Fatal("jj requested a Git branch")
	}
}

func TestDashboardKeepsSmallWorkspaceCompact(t *testing.T) {
	m := fixtureDashboard()
	m.width = 120
	m.height = 60
	view := m.View().Content
	lines := strings.Split(strings.TrimSpace(view), "\n")
	if len(lines) > 26 {
		t.Fatalf("two branches stretched across %d rows; controls should stay near the work", len(lines))
	}
	if strings.Contains(view, "[+]") {
		t.Fatal("decorative house still occupies the header")
	}
}

func TestDashboardUsesNormalTerminalScreen(t *testing.T) {
	if fixtureDashboard().View().AltScreen {
		t.Fatal("workspace still takes over the full terminal")
	}
}
