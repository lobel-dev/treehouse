package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
		{ID: "a", Title: "feature/庭", Status: "dirty", Details: "Current branch\nPath: /trees/one", Action: Action{Kind: ResumeBranch, Target: "a"}},
		{ID: "b", Title: "feature/quiet", Status: "available", Details: "Last used: feature/old", Action: Action{Kind: ResumeBranch, Target: "b"}},
	}}
	m.cache[HomePage] = m.snapshot
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

func TestDashboardNavigateDoesNotShowForeignSnapshot(t *testing.T) {
	m := fixtureDashboard()
	m.snapshot.Summary = "8 trees · 1 available · 7 leased"
	m.cache[HomePage] = m.snapshot
	m.navigate(CleanupPage)
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "8 trees · 1 available") {
		t.Fatal("cleanup showed the home summary")
	}
	special(m, tea.KeyEnter)
	if m.page != CleanupPage || m.exiting || m.action.Kind != NoAction {
		t.Fatalf("enter finished before cleanup loaded: page=%v exiting=%v action=%+v", m.page, m.exiting, m.action)
	}
}

func TestDashboardTreesLoadErrorDoesNotResumeHome(t *testing.T) {
	m := fixtureDashboard()
	m.navigate(TreesPage)
	m.Update(snapshotMsg{generation: m.generation, err: errors.New("cannot read pool")})
	special(m, tea.KeyEnter)
	if m.action.Kind == ResumeBranch || m.exiting {
		t.Fatalf("enter used leftover home action: %+v", m.action)
	}
}
func TestDashboardCleanupEnterRemoves(t *testing.T) {
	m := fixtureDashboard()
	m.page = CleanupPage
	m.snapshot.CandidatePaths = []string{"/only/displayed"}
	m.cache[CleanupPage] = m.snapshot
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "remove") || strings.Contains(view, "Tab  switch to Remove") || strings.Contains(view, "cancel — nothing will be removed") {
		t.Fatalf("cleanup footer hides remove: %q", view)
	}
	special(m, tea.KeyEnter)
	if m.action.Kind != RemoveTrees || len(m.action.Paths) != 1 || m.action.Paths[0] != "/only/displayed" {
		t.Fatalf("enter did not remove displayed trees: %+v", m.action)
	}
	m = fixtureDashboard()
	m.page = CleanupPage
	m.snapshot.CandidatePaths = []string{"/only/displayed"}
	m.cache[CleanupPage] = m.snapshot
	special(m, tea.KeyEscape)
	if m.action.Kind != NoAction || m.page != HomePage || m.exiting {
		t.Fatal("escape removed trees")
	}
	m = fixtureDashboard()
	m.options.Page = CleanupPage
	m.page = CleanupPage
	m.snapshot.CandidatePaths = []string{"/only/displayed"}
	m.cache[CleanupPage] = m.snapshot
	special(m, tea.KeyEscape)
	if m.action.Kind != NoAction || !m.exiting {
		t.Fatal("standalone escape did not quit")
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
				m.snapshot.Rows[0].Title = strings.Repeat("庭é", 100)
				m.snapshot.Rows[0].Details = strings.Repeat("Long details about 庭 work\n", 60)
				m.cache[HomePage] = m.snapshot
				view := m.View().Content
				if lipgloss.Height(view) != m.height {
					t.Fatalf("height %d at %d: %d", lipgloss.Height(view), width, m.height)
				}
				for _, line := range strings.Split(view, "\n") {
					if lipgloss.Width(line) > width {
						t.Fatalf("width overflow at %d: %d", width, lipgloss.Width(line))
					}
				}
				if mono {
					if !strings.Contains(view, "[dirty]") {
						t.Fatal("long branch hid its status badge")
					}
					if strings.Contains(view, "\x1b") {
						t.Fatal("NO_COLOR emitted style escapes")
					}
				} else if !strings.Contains(view, "dirty") || strings.Contains(view, "[dirty]") {
					t.Fatal("color mode hid dirty or used a mono pill")
				}
				if !strings.Contains(view, "Enter") || !strings.Contains(view, "q") {
					t.Fatal("footer absent")
				}
			}
		}
	}
}
func TestDashboardEmptyErrorAndNoMatches(t *testing.T) {
	m := fixtureDashboard()
	m.snapshot.Rows = nil
	m.cache[HomePage] = m.snapshot
	view := m.View().Content
	if !strings.Contains(view, "No branches") || !strings.Contains(view, "Press n") {
		t.Fatal("empty state missing")
	}
	m = fixtureDashboard()
	press(m, '/')
	m.input.SetValue("not found")
	if !strings.Contains(m.View().Content, "No matches") {
		t.Fatal("no-match state missing")
	}
	m = fixtureDashboard()
	m.page = TreesPage
	m.snapshot.Rows = nil
	m.cache[TreesPage] = m.snapshot
	trees := ansi.Strip(m.View().Content)
	if !strings.Contains(trees, "No trees") {
		t.Fatal("empty trees missing")
	}
	if strings.Contains(trees, "start a branch") || strings.Contains(trees, "n new") {
		t.Fatal("empty trees advertised a dead n")
	}
	m = fixtureDashboard()
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

func TestDashboardPastedControlsAreSanitized(t *testing.T) {
	payload := "feat/\x1b]0;pwned\x07evil\x1b[31mred\n\tname"
	m := fixtureDashboard()
	press(m, 'n')
	m.Update(tea.PasteMsg{Content: payload})
	if got := m.input.Value(); got != "feat/evilredname" {
		t.Fatalf("branch paste left controls or dropped text: %q", got)
	}
	view := m.View().Content
	if strings.Contains(view, "\x1b]0") || strings.Contains(view, "\x1b[31m") || strings.Contains(view, "\x07") {
		t.Fatalf("pasted controls reached the terminal: %q", view)
	}

	m = fixtureDashboard()
	press(m, '/')
	m.Update(tea.PasteMsg{Content: "quiet\x1b[0m"})
	if got := m.input.Value(); got != "quiet" {
		t.Fatalf("search paste left CSI: %q", got)
	}
	if rows := m.rows(); len(rows) != 1 || rows[0].ID != "b" {
		t.Fatalf("sanitized search failed to match: %+v", rows)
	}

	m = fixtureDashboard()
	press(m, 'n')
	m.Update(tea.PasteMsg{Content: "feat/\u202eevil\u2028name"})
	if got := m.input.Value(); got != "feat/evilname" {
		t.Fatalf("bidi or line separator survived paste: %q", got)
	}
	m = fixtureDashboard()
	m.snapshot.Rows[0].Title = "feature/\u202e123\u2028hidden"
	m.cache[HomePage] = m.snapshot
	view = ansi.Strip(m.View().Content)
	if strings.Contains(view, "\u202e") || strings.Contains(view, "\u2028") {
		t.Fatal("format controls reached the frame")
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
		m.snapshot.Rows = append(m.snapshot.Rows, DashboardRow{ID: "extra", Title: "other work", Status: "available"})
	}
	m.snapshot.Rows[len(m.snapshot.Rows)-1].Title = "last branch"
	m.cache[HomePage] = m.snapshot
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
	m = fixtureDashboard()
	m.options.JJ = true
	press(m, 't')
	if m.page != HomePage {
		t.Fatal("jj t navigated away from home")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "cleanup") {
		t.Fatal("jj home hid cleanup")
	}
	if strings.Contains(view, "t trees") {
		t.Fatal("jj home advertised t")
	}
}

func TestDashboardSearchMatchesVisibleStatus(t *testing.T) {
	m := fixtureDashboard()
	m.snapshot.Rows = []DashboardRow{{ID: "p", Title: "busy", Status: "in-use", Action: Action{Kind: OpenTree, Name: "1"}}}
	m.cache[HomePage] = m.snapshot
	press(m, '/')
	m.input.SetValue("in use")
	if len(m.rows()) != 1 || m.rows()[0].ID != "p" {
		t.Fatal("visible status pill did not match")
	}
}

func TestDashboardFillsTerminalHeight(t *testing.T) {
	for _, size := range []struct{ h, w int }{{60, 120}, {24, 80}, {20, 32}} {
		for _, page := range []Page{HomePage, TreesPage, CleanupPage} {
			m := fixtureDashboard()
			m.width, m.height, m.page = size.w, size.h, page
			if page == CleanupPage {
				m.snapshot.Notice = "Remove the listed trees? Git branches are kept."
				m.snapshot.CandidatePaths = []string{"/tree"}
			}
			m.cache[page] = m.snapshot
			view := m.View().Content
			if lipgloss.Height(view) != m.height {
				t.Fatalf("%dx%d page %v height %d", size.w, size.h, page, lipgloss.Height(view))
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > m.width {
					t.Fatalf("%dx%d overflow %d", size.w, size.h, lipgloss.Width(line))
				}
			}
			if !strings.Contains(view, "Enter") || !strings.Contains(view, "q") {
				t.Fatalf("%dx%d missing footer", size.w, size.h)
			}
			if page == HomePage && (size.w >= 80) {
				if !strings.Contains(view, "feature/庭") || !strings.Contains(view, "feature/quiet") {
					t.Fatalf("titles hidden at %dx%d: %q", size.w, size.h, ansi.Strip(view))
				}
				if strings.Contains(view, "│ feature/庭") || strings.Contains(view, "│ feature/quiet") {
					t.Fatal("details stole list rows with under-row marks")
				}
			}
		}
	}
}

func TestDashboardUsesAlternateScreen(t *testing.T) {
	m := fixtureDashboard()
	if !m.View().AltScreen {
		t.Fatal("home is not alt-screen")
	}
	m.page = TreesPage
	if !m.View().AltScreen {
		t.Fatal("trees is not alt-screen")
	}
	m.page = CleanupPage
	if !m.View().AltScreen {
		t.Fatal("cleanup is not alt-screen")
	}
	m.help = true
	if !m.View().AltScreen {
		t.Fatal("help is not alt-screen")
	}
	m.help = false
	m.searching = true
	if !m.View().AltScreen {
		t.Fatal("search is not alt-screen")
	}
	m.searching = false
	m.finish(Action{})
	if !m.View().AltScreen {
		t.Fatal("finish dropped alt-screen")
	}
}

func TestDashboardPinnedDetailsDoNotHideRows(t *testing.T) {
	m := fixtureDashboard()
	m.width, m.height = 80, 24
	m.snapshot.Rows[0].Details = m.snapshot.Rows[0].Title + "\n" + strings.Repeat("session detail line that is quite long\n", 12)
	m.cache[HomePage] = m.snapshot
	view := m.View().Content
	if !strings.Contains(view, "feature/庭") || !strings.Contains(view, "feature/quiet") {
		t.Fatal("pinned details hid a title")
	}
	if strings.Contains(view, "│ "+m.snapshot.Rows[0].Title) {
		t.Fatal("details panel repeated Title")
	}
}

func TestDashboardFooterNamesAction(t *testing.T) {
	m := fixtureDashboard()
	m.width, m.height = 80, 24
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Enter") || !strings.Contains(view, "resume") || !strings.Contains(view, "feature/庭") {
		t.Fatalf("primary footer did not name the action: %q", view)
	}
}

func TestDashboardGitChromeHasNoJJCopy(t *testing.T) {
	m := fixtureDashboard()
	views := []string{m.View().Content}
	m.help = true
	views = append(views, m.View().Content)
	for _, view := range views {
		lower := strings.ToLower(ansi.Strip(view))
		for _, banned := range []string{"workspaces", "start tree", "jj workspace", "scroll selected details", "keyboard shortcuts"} {
			if strings.Contains(lower, banned) {
				t.Fatalf("git chrome contains %q", banned)
			}
		}
	}
}

func TestDashboardHeaderBrand(t *testing.T) {
	m := fixtureDashboard()
	m.width, m.height = 80, 24
	view := m.View().Content
	if !strings.Contains(view, "treehouse") {
		t.Fatal("header missing brand")
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("brand overflow: %d", lipgloss.Width(line))
		}
	}
}

func TestDashboardFooterAlwaysPresent(t *testing.T) {
	states := []struct {
		name string
		edit func(*dashboard)
	}{
		{"home", func(*dashboard) {}},
		{"trees", func(m *dashboard) { m.page = TreesPage; m.cache[TreesPage] = m.snapshot }},
		{"cleanup", func(m *dashboard) {
			m.page = CleanupPage
			m.snapshot.CandidatePaths = []string{"/only/displayed"}
			m.cache[CleanupPage] = m.snapshot
		}},
		{"loading", func(m *dashboard) { m.loading = true }},
		{"help", func(m *dashboard) { m.help = true }},
	}
	for _, size := range []struct{ h, w int }{{24, 80}, {20, 32}} {
		for _, state := range states {
			m := fixtureDashboard()
			m.width, m.height = size.w, size.h
			state.edit(m)
			view := m.View().Content
			if lipgloss.Height(view) != m.height {
				t.Fatalf("%s %dx%d height %d", state.name, size.w, size.h, lipgloss.Height(view))
			}
			if !strings.Contains(view, "Enter") || !strings.Contains(view, "quit") {
				t.Fatalf("%s %dx%d missing footer", state.name, size.w, size.h)
			}
		}
	}
}

func TestDashboardCleanupFooterShortcutLabels(t *testing.T) {
	m := fixtureDashboard()
	m.width, m.height = 80, 24
	m.page = CleanupPage
	m.snapshot.CandidatePaths = []string{"/only/displayed"}
	m.cache[CleanupPage] = m.snapshot
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Esc cancel") || !strings.Contains(view, "q quit") {
		t.Fatalf("cleanup with candidates missing Esc cancel / q quit: %q", view)
	}
	m.snapshot.CandidatePaths = nil
	m.cache[CleanupPage] = m.snapshot
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Esc back") {
		t.Fatalf("cleanup with no candidates missing Esc back: %q", view)
	}
}

func TestDashboardBannerNotAPage(t *testing.T) {
	m := fixtureDashboard()
	m.width, m.height = 80, 24
	m.options.Banner = strings.Repeat("Removed lots.\n", 20)
	view := m.View().Content
	if strings.Count(view, "Removed lots.") > 1 {
		t.Fatal("full cleanup result entered the workspace")
	}
	if !strings.Contains(view, "feature/庭") || !strings.Contains(view, "feature/quiet") {
		t.Fatal("banner replaced the branch list")
	}
}

func TestDashboardCleanupOrphanWarning(t *testing.T) {
	m := fixtureDashboard()
	m.width, m.height = 32, 20
	m.page = CleanupPage
	m.snapshot.Notice = "WARNING: orphan contents could not be verified.\nRemove the listed trees? Git branches are kept."
	m.snapshot.CandidatePaths = []string{"/orphan"}
	m.cache[CleanupPage] = m.snapshot
	view := m.View().Content
	flat := strings.Join(strings.Fields(ansi.Strip(view)), " ")
	if !strings.Contains(flat, "WARNING: orphan") || !strings.Contains(flat, "Git branches are kept") {
		t.Fatalf("orphan warning or confirm copy missing: %q", flat)
	}
	if lipgloss.Height(view) != 20 {
		t.Fatalf("height %d", lipgloss.Height(view))
	}
}

func TestDashboardHardwareCursor(t *testing.T) {
	m := fixtureDashboard()
	m.width, m.height = 80, 24
	if m.View().Cursor != nil {
		t.Fatal("unfocused cursor")
	}
	press(m, '/')
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("search cursor missing")
	}
	findY := -1
	for i, line := range strings.Split(v.Content, "\n") {
		if strings.Contains(ansi.Strip(line), "Find:") {
			findY = i
			break
		}
	}
	if findY < 0 || v.Cursor.Position.Y != findY {
		t.Fatalf("search cursor Y=%d findY=%d", v.Cursor.Position.Y, findY)
	}
	wantX := 1 + lipgloss.Width("Find: ") + lipgloss.Width(m.input.Prompt)
	if v.Cursor.Position.X != wantX {
		t.Fatalf("search cursor X=%d want %d", v.Cursor.Position.X, wantX)
	}

	m = fixtureDashboard()
	m.options.Repository = strings.Repeat("very-long-repository-name", 4)
	m.width, m.height = 32, 24
	press(m, '/')
	v = m.View()
	if v.Cursor == nil {
		t.Fatal("wrapped header search cursor missing")
	}
	findY = -1
	for i, line := range strings.Split(v.Content, "\n") {
		if strings.Contains(ansi.Strip(line), "Find:") {
			findY = i
			break
		}
	}
	if findY < 0 || v.Cursor.Position.Y != findY {
		t.Fatalf("wrapped search Y=%d findY=%d", v.Cursor.Position.Y, findY)
	}

	m = fixtureDashboard()
	press(m, 'n')
	m.width, m.height = 80, 1
	v = m.View()
	if v.Cursor != nil {
		t.Fatal("off-screen field used a hardware cursor")
	}
	if !m.input.VirtualCursor() {
		t.Fatal("off-screen field did not use a virtual cursor")
	}
}

func TestDashboardHelpQQuits(t *testing.T) {
	m := fixtureDashboard()
	press(m, '?')
	if !m.help {
		t.Fatal("help not open")
	}
	press(m, 'q')
	if !m.exiting || m.action.Kind != NoAction {
		t.Fatal("q in help did not quit the workspace")
	}
	m = fixtureDashboard()
	press(m, '?')
	special(m, tea.KeyEscape)
	if m.help || m.exiting {
		t.Fatal("esc did not close help")
	}
}

func TestDashboardEnterUsesVisibleSnapshotWhileLoading(t *testing.T) {
	m := fixtureDashboard()
	m.Update(snapshotMsg{generation: m.generation, snapshot: m.snapshot})
	m.reload()
	if !m.loading {
		t.Fatal("reload did not start a load")
	}
	special(m, tea.KeyEnter)
	if m.action.Kind != ResumeBranch || m.action.Target != "a" {
		t.Fatalf("enter ignored visible rows: %+v", m.action)
	}
}

func TestDashboardLoadErrorKeepsCache(t *testing.T) {
	m := fixtureDashboard()
	m.Update(snapshotMsg{generation: m.generation, snapshot: m.snapshot})
	m.reload()
	m.Update(snapshotMsg{generation: m.generation, err: errors.New("cannot read pool")})
	view := m.View().Content
	if !strings.Contains(view, "feature/庭") {
		t.Fatal("error dropped the cached list")
	}
	if !strings.Contains(view, "cannot read pool") {
		t.Fatal("error missing")
	}
}
