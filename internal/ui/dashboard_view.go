package ui

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Repository text cannot supply terminal controls or styling.
func dashboardText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

type workspacePalette struct{ text, muted, accent, selected, warning string }

func (m *dashboard) palette() workspacePalette {
	if m.dark {
		return workspacePalette{"#E4E7EB", "#909AA8", "#70D6AD", "#203C35", "#F0BE78"}
	}
	return workspacePalette{"#222D38", "#586676", "#087D61", "#D9F0E6", "#976017"}
}
func (m *dashboard) ink(s, color string, bold bool) string {
	if m.monochrome {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(bold).Render(s)
}
func (m *dashboard) hint(s string) string    { return m.ink(s, m.palette().muted, false) }
func (m *dashboard) primary(s string) string { return m.ink(s, m.palette().accent, true) }
func clip(s string, width int) string        { return ansi.Truncate(s, max(1, width), "…") }
func wrapped(s string, width int) string     { return ansi.Wrap(s, max(1, width), "") }
func window(s string, width, height, offset int) string {
	lines := strings.Split(wrapped(s, width), "\n")
	start := min(offset, max(0, len(lines)-1))
	return strings.Join(lines[start:min(len(lines), start+max(1, height))], "\n")
}
func (m *dashboard) footer(width int) string {
	primary, secondary := "Enter resume  / search  n new branch", "j/k move  t trees  c cleanup  r refresh  ? help  q quit"
	if m.options.JJ {
		primary = "Enter open  / search  n start tree"
	}
	if m.page == TreesPage {
		primary = "Enter open  / search  Esc back"
		secondary = "j/k move  r refresh  ? help  q quit"
	}
	if m.page == CleanupPage {
		primary = "Tab choose  Enter confirm  Esc back"
		secondary = "j/k inspect  r refresh  ? help  q quit"
	}
	if m.searching {
		primary = "Type to filter  Enter open  Esc clear"
		secondary = "Up/Down select  Ctrl+C quit"
	}
	if m.creating {
		primary = "Enter create branch  Esc back"
		secondary = ""
	}
	if m.page == ResultPage {
		primary = "Enter continue  q quit"
		secondary = "PgUp/PgDn scroll"
	}
	if m.help {
		primary = "? or Esc close help  Ctrl+C quit"
		secondary = "PgUp/PgDn scroll"
	}
	result := wrapped(m.shortcuts(primary), width)
	if secondary != "" {
		result += "\n" + m.hint(wrapped(secondary, width))
	}
	return result
}
func (m *dashboard) shortcuts(s string) string {
	var groups []string
	for _, part := range strings.Split(s, "  ") {
		key, label, _ := strings.Cut(part, " ")
		groups = append(groups, m.primary(key)+" "+m.hint(label))
	}
	return strings.Join(groups, "  ")
}
func (m *dashboard) statusColor(status string) string {
	switch status {
	case "dirty", "protected":
		return m.palette().warning
	case "leased":
		if m.dark {
			return "#B6A5F5"
		}
		return "#7253B3"
	case "in use":
		if m.dark {
			return "#83BDF0"
		}
		return "#2167A1"
	default:
		return m.palette().accent
	}
}
func (m *dashboard) row(r DashboardRow, width int, selected bool) string {
	badge := "[" + dashboardText(r.Status) + "]"
	labelWidth := max(1, width-lipgloss.Width(badge)-4)
	label := clip(strings.ReplaceAll(dashboardText(r.Label), "\n", " "), labelWidth)
	prefix := "  "
	if selected {
		prefix = "> "
	}
	line := prefix + label + strings.Repeat(" ", max(1, width-2-lipgloss.Width(label)-lipgloss.Width(badge))) + badge
	line = clip(line, width)
	if m.monochrome {
		return line
	}
	p := m.palette()
	gap := strings.Repeat(" ", max(1, width-2-lipgloss.Width(label)-lipgloss.Width(badge)))
	styled := m.ink(prefix+label, p.text, selected) + gap + m.ink(badge, m.statusColor(r.Status), false)
	if selected {
		return lipgloss.NewStyle().Background(lipgloss.Color(p.selected)).Foreground(lipgloss.Color(p.text)).Bold(true).Render(line)
	}
	return clip(styled, width)
}
func (m *dashboard) details(row DashboardRow, width, height int) string {
	details := dashboardText(row.Details)
	lines := strings.Split(details, "\n")
	// The selected row already supplies its name; preserve it in the details
	// when the row had to truncate it.
	if len(lines) > 1 && (lines[0] == row.Action.Target || lines[0] == row.Label) && lipgloss.Width(row.Label) < width-18 {
		details = strings.Join(lines[1:], "\n")
	}
	text := window(details, width-4, height, m.detailOffset)
	var rendered []string
	for _, line := range strings.Split(text, "\n") {
		rendered = append(rendered, m.hint("  │ "+line))
	}
	return strings.Join(rendered, "\n")
}
func (m *dashboard) list(width, budget int) string {
	rows := m.rows()
	if m.loading {
		return m.hint("Loading...")
	}
	if m.err != "" {
		return m.ink(window("Could not load workspace.\n"+dashboardText(m.err)+"\nPress r to retry.", width, budget, 0), m.palette().warning, false)
	}
	if len(rows) == 0 {
		message := "No available branches.\nPress n to start work, or t to browse trees."
		if m.page == TreesPage {
			message = "No trees yet.\nStart work from home to create one."
		}
		if m.page == CleanupPage {
			message = "Nothing to clean up. Your work is kept."
		}
		if m.searching && m.input.Value() != "" {
			message = "No matches.\nEscape clears search."
		}
		return m.hint(window(message, width, budget, 0))
	}
	selected := min(m.selected, len(rows)-1)
	detailHeight := min(5, max(0, budget-3))
	details := m.details(rows[selected], width, detailHeight)
	if detailHeight == 0 {
		details = ""
	} else {
		detailHeight = lipgloss.Height(details)
	}
	rowLimit := min(8, max(1, budget-detailHeight-1))
	start := max(0, selected-rowLimit+1)
	end := min(len(rows), start+rowLimit)
	var lines []string
	for i := start; i < end; i++ {
		lines = append(lines, m.row(rows[i], width, i == selected))
		if i == selected && details != "" {
			lines = append(lines, details)
		}
	}
	if len(rows) > rowLimit {
		lines = append(lines, m.hint(fmt.Sprintf("  %d–%d of %d · j/k to scroll", start+1, end, len(rows))))
	}
	return strings.Join(lines, "\n")
}
func (m *dashboard) View() tea.View {
	width := max(1, min(96, m.width-2))
	title := m.primary("treehouse") + m.hint(" / ") + m.ink(dashboardText(m.options.Repository), m.palette().text, true)
	if m.exiting {
		message := "Closed."
		switch m.action.Kind {
		case ResumeBranch, CreateBranch:
			message = "Opening " + dashboardText(m.action.Target) + "…"
		case OpenTree:
			message = "Opening tree " + dashboardText(m.action.Name) + "…"
		case StartTree:
			message = "Starting a tree…"
		case RemoveTrees:
			message = "Cleaning up selected trees…"
		}
		return tea.NewView(" " + clip(title+"  "+m.hint(message), width))
	}
	header := clip(title, width)
	section := "LOCAL BRANCHES"
	if m.options.JJ {
		section = "WORKSPACES"
	}
	if m.page == TreesPage {
		section = "TREES"
	}
	if m.page == CleanupPage {
		section = "CLEANUP"
	}
	if m.creating {
		section = "NEW BRANCH"
	}
	if m.page == ResultPage {
		section = "CLEANUP RESULT"
	}
	if m.help {
		section = "KEYBOARD SHORTCUTS"
	}
	summary := dashboardText(m.snapshot.Summary)
	if !m.creating && !m.help && m.page != ResultPage {
		section += fmt.Sprintf("  %d", len(m.rows()))
	}
	subtitle := m.hint(section)
	if summary != "" && width >= 68 && !m.creating && !m.help && m.page != ResultPage {
		gap := width - lipgloss.Width(section) - lipgloss.Width(summary)
		if gap >= 3 {
			subtitle += strings.Repeat(" ", gap) + m.hint(summary)
		}
	}
	header += "\n" + clip(subtitle, width)
	footer := m.footer(width)
	budget := max(1, m.height-lipgloss.Height(header)-lipgloss.Height(footer)-4)
	body := ""
	switch {
	case m.creating:
		body = "Branch name\n" + m.input.View() + "\n" + m.hint(strings.Repeat("─", min(48, width)))
		if m.validating {
			body += "\n" + m.hint("Checking branch name...")
		} else if m.err != "" {
			body += "\n" + m.ink(dashboardText(m.err), m.palette().warning, false)
		}
	case m.help:
		body = "Enter     Open the selected branch or tree\n/         Filter the list; Escape clears it\nj/k       Move selection (or use arrows)\nn         Create a branch; start a tree for jj\nt         Browse existing trees\nc         Preview cleanup; Cancel is the default\nr         Refresh the current list\nPgUp/Dn   Scroll selected details\nEsc       Go back or cancel\nq         Quit outside text entry\n\nExit the opened shell to return to your terminal."
		body = m.hint(window(body, width, budget, m.detailOffset))
	case m.page == ResultPage:
		body = window(dashboardText(m.options.Result), width, budget, m.detailOffset)
	default:
		prefix := ""
		if m.searching {
			prefix = "Search: " + m.input.View() + "\n"
		}
		if m.page == CleanupPage {
			choice := m.primary("> Cancel") + m.hint("     Remove listed trees")
			if m.confirm {
				choice = m.hint("  Cancel     ") + m.ink("> Remove listed trees", m.palette().warning, true)
			}
			prefix = wrapped(dashboardText(m.snapshot.Notice), width) + "\n" + choice + "\n\n"
		}
		body = prefix + m.list(width, max(1, budget-lipgloss.Height(strings.TrimSuffix(prefix, "\n"))))
	}
	body = window(body, width, budget, 0)
	content := header + "\n\n" + body + "\n\n" + footer
	// Normal-screen rendering keeps the invoking command and terminal history.
	// The widget grows with its content rather than stretching to terminal height.
	frame := lipgloss.NewStyle().PaddingLeft(1)
	if !m.monochrome {
		frame = frame.Foreground(lipgloss.Color(m.palette().text))
	}
	return tea.NewView(frame.Render(content))
}
