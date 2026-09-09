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
	ResultPage
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
	ID, Label, Status, Details string
	Action                     Action
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
	Result     string
	// Selection carries navigation across a cleanup execution, never persisted.
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
	options                                        DashboardOptions
	page                                           Page
	snapshot                                       DashboardSnapshot
	generation                                     int
	loading                                        bool
	err                                            string
	selected, detailOffset                         int
	selection                                      map[Page]string
	input                                          textinput.Model
	searching, creating, validating, help, confirm bool
	width, height                                  int
	dark, monochrome                               bool
	action                                         Action
	exiting                                        bool
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
	_, mono := os.LookupEnv("NO_COLOR")
	if mono {
		input.SetStyles(textinput.Styles{})
	}
	page := o.Page
	if o.Result != "" {
		page = ResultPage
	}
	selection := o.Selection
	if selection == nil {
		selection = map[Page]string{}
	}
	return &dashboard{options: o, page: page, input: input, width: 80, height: 24, dark: true, monochrome: mono, selection: selection}
}
func (m *dashboard) Init() tea.Cmd {
	if m.page == ResultPage {
		return tea.RequestBackgroundColor
	}
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
	m.loading, m.confirm, m.err = true, false, ""
	return func() tea.Msg { s, err := load(page); return snapshotMsg{generation, s, err} }
}
func (m *dashboard) navigate(page Page) tea.Cmd {
	m.remember()
	m.page, m.selected, m.detailOffset = page, 0, 0
	m.snapshot = DashboardSnapshot{}
	m.searching, m.creating, m.validating = false, false, false
	m.input.SetValue("")
	m.input.Blur()
	return m.reload()
}
func (m *dashboard) rows() []DashboardRow {
	if !m.searching || m.input.Value() == "" {
		return m.snapshot.Rows
	}
	var rows []DashboardRow
	for _, r := range m.snapshot.Rows {
		if strings.Contains(strings.ToLower(r.Label+" "+r.Status), strings.ToLower(m.input.Value())) {
			rows = append(rows, r)
		}
	}
	return rows
}
func (m *dashboard) finish(a Action) (tea.Model, tea.Cmd) {
	m.remember()
	m.action = a
	m.exiting = true
	return m, tea.Quit
}
func (m *dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
		m.input.SetWidth(max(1, min(96, m.width-2)-10))
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
			m.snapshot = DashboardSnapshot{}
			return m, nil
		}
		m.snapshot = msg.snapshot
		m.selected = 0
		for i, r := range m.rows() {
			if r.ID == m.selection[m.page] {
				m.selected = i
				break
			}
		}
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
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.selected, m.detailOffset = 0, 0
			return m, cmd
		}
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m.finish(Action{})
		}
		if m.help {
			if key == "pgdown" {
				m.detailOffset++
			}
			if key == "pgup" {
				m.detailOffset = max(0, m.detailOffset-1)
			}
			if key == "?" || key == "esc" || key == "q" {
				m.help = false
				m.detailOffset = 0
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
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if m.searching {
			switch key {
			case "esc":
				m.searching = false
				m.input.SetValue("")
				m.input.Blur()
				m.selected = 0
				return m, nil
			case "enter", "up", "down", "pgup", "pgdown": // navigation remains available while filtering
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.selected = 0
				m.detailOffset = 0
				return m, cmd
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
			if m.page != ResultPage {
				return m, m.reload()
			}
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
			if m.page == HomePage {
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
		case "tab", "left", "right":
			if m.page == CleanupPage && !m.loading && m.err == "" && len(m.snapshot.CandidatePaths) > 0 {
				m.confirm = !m.confirm
			}
		case "enter":
			if m.page == ResultPage {
				if m.options.Page == CleanupPage || m.options.Page == ResultPage {
					return m.finish(Action{})
				}
				return m, m.navigate(HomePage)
			}
			if m.loading || m.err != "" {
				return m, nil
			}
			if m.page == CleanupPage {
				if m.confirm && len(m.snapshot.CandidatePaths) > 0 {
					return m.finish(Action{Kind: RemoveTrees, Paths: append([]string{}, m.snapshot.CandidatePaths...)})
				}
				m.options.Result = "Canceled. Nothing removed."
				m.page = ResultPage
				m.detailOffset = 0
				return m, nil
			}
			rows := m.rows()
			if m.selected < len(rows) {
				return m.finish(rows[m.selected].Action)
			}
		}
	}
	return m, nil
}
