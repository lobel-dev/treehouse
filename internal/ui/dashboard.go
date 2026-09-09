package ui

import (
	"os"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-isatty"
)

// Dashboard data is presentation-only. Commands own all repository operations.
type Page int

const (
	HomePage Page = iota
	TreesPage
	CleanupPage
)

type ActionKind int

const (
	NoAction ActionKind = iota
	ResumeBranch
	CreateBranch
	OpenTree
	StartTree
	RemoveTrees
)

type Action struct {
	Kind   ActionKind
	Target string
	Name   string
	Paths  []string
}
type DashboardRow struct {
	ID, Title, Annotation, Status, Details string
	Action                                 Action
}
type DashboardSnapshot struct {
	Rows            []DashboardRow
	Summary, Notice string
	CandidatePaths  []string
}
type DashboardOptions struct {
	Repository string
	JJ         bool
	Page       Page
	Banner     string
	// BannerWarning paints Banner in warning color; the view does not parse the text.
	BannerWarning  bool
	Selection      map[Page]string
	Load           func(Page) (DashboardSnapshot, error)
	ValidateBranch func(string) error
}
type snapshotMsg struct {
	generation int
	snapshot   DashboardSnapshot
	err        error
}
type validationMsg struct {
	generation int
	name       string
	err        error
}
type dashboard struct {
	options                               DashboardOptions
	page                                  Page
	snapshot                              DashboardSnapshot
	cache                                 map[Page]DashboardSnapshot
	generation                            int
	loading                               bool
	err                                   string
	selected, detailOffset                int
	selection                             map[Page]string
	input                                 textinput.Model
	searching, creating, validating, help bool
	width, height                         int
	dark, monochrome                      bool
	action                                Action
	exiting                               bool
}

// DashboardSupported excludes terminals without a cancellable raw input path.
func DashboardSupported() bool {
	// Bubble Tea's Windows reader opens CONIN$ for non-console input.
	// MSYS/MinTTY pipes therefore use the existing line-oriented interface.
	return IsInteractive() && os.Getenv("TERM") != "dumb" && (runtime.GOOS != "windows" || isatty.IsTerminal(os.Stdin.Fd()))
}

// RunDashboard returns only after Bubble Tea has stopped its input reader and
// restored the terminal. Callers may then execute the typed request or a shell.
func RunDashboard(options DashboardOptions) (Action, error) {
	m := newDashboard(options)
	result, err := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr)).Run()
	if err != nil {
		return Action{}, err
	}
	return result.(*dashboard).action, nil
}
func newDashboard(o DashboardOptions) *dashboard {
	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 0
	input.SetWidth(68)
	input.SetVirtualCursor(false)
	_, mono := os.LookupEnv("NO_COLOR")
	if mono {
		input.SetStyles(textinput.Styles{})
	}
	selection := o.Selection
	if selection == nil {
		selection = map[Page]string{}
	}
	return &dashboard{options: o, page: o.Page, input: input, width: 80, height: 24, dark: true, monochrome: mono, selection: selection, cache: map[Page]DashboardSnapshot{}}
}
func (m *dashboard) Init() tea.Cmd {
	return tea.Batch(m.reload(), tea.RequestBackgroundColor)
}
func (m *dashboard) remember() {
	rows := m.rows()
	if m.selected < len(rows) {
		m.selection[m.page] = rows[m.selected].ID
	}
}
func (m *dashboard) reload() tea.Cmd {
	m.remember()
	m.generation++
	generation, page, load := m.generation, m.page, m.options.Load
	m.loading, m.err = true, ""
	return func() tea.Msg { s, err := load(page); return snapshotMsg{generation, s, err} }
}
func (m *dashboard) navigate(page Page) tea.Cmd {
	m.remember()
	m.page, m.selected, m.detailOffset = page, 0, 0
	m.searching, m.creating, m.validating, m.help = false, false, false, false
	m.input.SetValue("")
	m.input.Blur()
	if cached, ok := m.cache[page]; ok {
		m.snapshot = cached
		for i, r := range m.rows() {
			if r.ID == m.selection[page] {
				m.selected = i
				break
			}
		}
	} else {
		m.snapshot = DashboardSnapshot{}
	}
	return m.reload()
}
func (m *dashboard) visible() DashboardSnapshot {
	if cached, ok := m.cache[m.page]; ok {
		return cached
	}
	return DashboardSnapshot{}
}
func (m *dashboard) pageReady() bool {
	_, ok := m.cache[m.page]
	return ok
}
func (m *dashboard) rows() []DashboardRow {
	if m.loading && !m.pageReady() {
		return nil
	}
	rows := m.visible().Rows
	if !m.searching || m.input.Value() == "" {
		return rows
	}
	var filtered []DashboardRow
	needle := strings.ToLower(m.input.Value())
	for _, r := range rows {
		hay := strings.ToLower(r.Title + " " + r.Annotation + " " + r.Status + " " + m.statusLabel(r.Status))
		if strings.Contains(hay, needle) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
func (m *dashboard) finish(a Action) (tea.Model, tea.Cmd) {
	m.remember()
	m.action = a
	m.exiting = true
	return m, tea.Quit
}
func (m *dashboard) leaveCleanup() (tea.Model, tea.Cmd) {
	if m.options.Page == CleanupPage {
		return m.finish(Action{})
	}
	return m, m.navigate(HomePage)
}
func (m *dashboard) acceptField(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if v := sanitizeField(m.input.Value()); v != m.input.Value() {
		m.input.SetValue(v)
	}
	return cmd
}
func (m *dashboard) restoreSelection() {
	m.selected = 0
	id := m.selection[m.page]
	for i, r := range m.rows() {
		if r.ID == id {
			m.selected = i
			break
		}
	}
}
func (m *dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
		m.input.SetWidth(max(1, max(1, m.width-2)-10))
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		if !m.monochrome {
			m.input.SetStyles(textinput.DefaultStyles(m.dark))
		}
	case snapshotMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.snapshot = msg.snapshot
		m.cache[m.page] = msg.snapshot
		m.restoreSelection()
	case validationMsg:
		if msg.generation != m.generation || !m.creating {
			return m, nil
		}
		m.validating = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		return m.finish(Action{Kind: CreateBranch, Target: msg.name})
	case tea.PasteMsg:
		if (m.creating || m.searching) && !m.validating {
			m.selected, m.detailOffset = 0, 0
			return m, m.acceptField(tea.PasteMsg{Content: sanitizeField(msg.Content)})
		}
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m.finish(Action{})
		}
		if key != "?" && m.options.Banner != "" {
			m.options.Banner = ""
			m.options.BannerWarning = false
		}
		if m.help {
			if key == "pgdown" {
				m.detailOffset++
			}
			if key == "pgup" {
				m.detailOffset = max(0, m.detailOffset-1)
			}
			if key == "?" || key == "esc" {
				m.help = false
				m.detailOffset = 0
				return m, nil
			}
			if key == "enter" {
				m.help = false
				m.detailOffset = 0
				return m, nil
			}
			if key == "q" {
				return m.finish(Action{})
			}
			return m, nil
		}
		if m.creating {
			if key == "esc" {
				m.generation++
				m.creating = false
				m.validating = false
				m.err = ""
				m.input.Blur()
				if m.loading {
					return m, m.reload()
				}
				return m, nil
			}
			if m.validating {
				return m, nil
			}
			if key == "enter" {
				name := strings.TrimSpace(m.input.Value())
				if name == "" {
					m.err = "Enter a branch name."
					return m, nil
				}
				m.validating = true
				generation, validate := m.generation, m.options.ValidateBranch
				return m, func() tea.Msg { return validationMsg{generation, name, validate(name)} }
			}
			return m, m.acceptField(msg)
		}
		if m.searching {
			switch key {
			case "esc":
				m.searching = false
				m.input.SetValue("")
				m.input.Blur()
				m.selected = 0
				return m, nil
			case "enter", "up", "down", "pgup", "pgdown":
			default:
				m.selected = 0
				m.detailOffset = 0
				return m, m.acceptField(msg)
			}
		}
		switch key {
		case "q":
			return m.finish(Action{})
		case "esc":
			if m.page == HomePage || m.page == m.options.Page {
				return m.finish(Action{})
			}
			return m, m.navigate(HomePage)
		case "?":
			m.help = true
			m.detailOffset = 0
		case "r":
			return m, m.reload()
		case "n":
			if m.page == HomePage {
				if m.options.JJ {
					return m.finish(Action{Kind: StartTree})
				}
				m.creating = true
				m.err = ""
				m.input.SetValue("")
				return m, m.input.Focus()
			}
		case "t":
			if m.page == HomePage && !m.options.JJ {
				return m, m.navigate(TreesPage)
			}
		case "c":
			if m.page == HomePage {
				return m, m.navigate(CleanupPage)
			}
		case "/":
			if m.page == HomePage || m.page == TreesPage {
				m.searching = true
				return m, m.input.Focus()
			}
		case "j", "down":
			m.selected = min(max(0, len(m.rows())-1), m.selected+1)
			m.detailOffset = 0
		case "k", "up":
			m.selected = max(0, m.selected-1)
			m.detailOffset = 0
		case "pgdown":
			m.detailOffset++
		case "pgup":
			m.detailOffset = max(0, m.detailOffset-1)
		case "enter":
			if !m.pageReady() {
				return m, nil
			}
			if m.page == CleanupPage {
				if len(m.visible().CandidatePaths) > 0 {
					return m.finish(Action{Kind: RemoveTrees, Paths: append([]string{}, m.visible().CandidatePaths...)})
				}
				return m.leaveCleanup()
			}
			rows := m.rows()
			if m.selected < len(rows) {
				return m.finish(rows[m.selected].Action)
			}
		}
	}
	return m, nil
}
