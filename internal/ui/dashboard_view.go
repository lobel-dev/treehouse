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
		if r == '\u2028' || r == '\u2029' {
			return '\n'
		}
		if unicode.Is(unicode.Cf, r) || unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

func sanitizeField(s string) string {
	return strings.ReplaceAll(dashboardText(s), "\n", "")
}

type workspacePalette struct {
	text, muted, accent, selected, warning, leased, inuse, border string
}

func (m *dashboard) palette() workspacePalette {
	if m.dark {
		return workspacePalette{
			text: "#E4E7EB", muted: "#909AA8", accent: "#70D6AD", selected: "#203C35",
			warning: "#F0BE78", leased: "#B6A5F5", inuse: "#83BDF0", border: "#4A8E75",
		}
	}
	return workspacePalette{
		text: "#222D38", muted: "#586676", accent: "#087D61", selected: "#D9F0E6",
		warning: "#976017", leased: "#7253B3", inuse: "#2167A1", border: "#5A9A82",
	}
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
	if height <= 0 {
		return ""
	}
	lines := strings.Split(wrapped(s, width), "\n")
	start := min(offset, max(0, len(lines)-1))
	return strings.Join(lines[start:min(len(lines), start+max(1, height))], "\n")
}

func blockHeight(s string) int {
	if s == "" {
		return 0
	}
	return lipgloss.Height(s)
}

func padBlock(s string, width, height int) string {
	if height <= 0 {
		return ""
	}
	if s == "" {
		return strings.Join(make([]string, height), "\n")
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = clip(lines[i], width)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func fitBlock(s string, width, height int) string {
	return padBlock(s, width, height)
}

func joinBlocks(blocks ...string) string {
	var parts []string
	for _, b := range blocks {
		if b != "" {
			parts = append(parts, b)
		}
	}
	return strings.Join(parts, "\n")
}

func oneLine(s string, width int) string {
	s = dashboardText(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return clip(strings.TrimSpace(s), width)
}

func (m *dashboard) contentWidth() int { return max(1, m.width-2) }

func (m *dashboard) brand() string {
	if !m.monochrome && lipgloss.Width("🌳") == 2 {
		return "🌳 treehouse"
	}
	return "treehouse"
}

func (m *dashboard) headerBar(width int) string {
	brand := m.primary(m.brand())
	repo := m.ink(dashboardText(m.options.Repository), m.palette().text, true)
	left := brand + m.hint("  ·  ") + repo
	summary := dashboardText(m.visible().Summary)
	if m.creating || m.help {
		summary = ""
	}
	if summary != "" && width >= 68 {
		gap := width - lipgloss.Width(ansi.Strip(left)) - lipgloss.Width(summary)
		if gap >= 3 {
			left += strings.Repeat(" ", gap) + m.hint(summary)
		}
	}
	return wrapped(left, width)
}

type bannerKind int

const (
	bannerNone bannerKind = iota
	bannerSearch
	bannerOrphan
	bannerUser
	bannerRefreshing
	bannerError
)

func orphanWarning(notice string) string {
	notice = dashboardText(notice)
	if !strings.HasPrefix(notice, "WARNING:") {
		return ""
	}
	line, _, _ := strings.Cut(notice, "\n")
	return strings.TrimSpace(line)
}

func (m *dashboard) currentBanner(dropUser, dropOrphan bool) bannerKind {
	if m.searching {
		return bannerSearch
	}
	if !dropOrphan && m.page == CleanupPage && !m.creating && !m.help {
		if orphanWarning(m.visible().Notice) != "" {
			return bannerOrphan
		}
	}
	if dropUser {
		return bannerNone
	}
	if m.options.Banner != "" && !m.creating && !m.help {
		return bannerUser
	}
	if m.loading && !m.creating && !m.help {
		if _, ok := m.cache[m.page]; ok {
			return bannerRefreshing
		}
	}
	if m.err != "" && !m.creating && !m.help {
		return bannerError
	}
	return bannerNone
}

func (m *dashboard) bannerLine(width int, kind bannerKind) string {
	p := m.palette()
	switch kind {
	case bannerSearch:
		return clip(m.hint("Find: ")+m.input.View(), width)
	case bannerOrphan:
		return m.ink(oneLine(orphanWarning(m.visible().Notice), width), p.warning, false)
	case bannerUser:
		line := oneLine(m.options.Banner, width)
		if m.options.BannerWarning {
			return m.ink(line, p.warning, false)
		}
		return m.hint(line)
	case bannerRefreshing:
		return m.hint(clip("Refreshing…", width))
	case bannerError:
		return m.ink(oneLine(m.err, width), p.warning, false)
	default:
		return ""
	}
}

func (m *dashboard) statusLabel(status string) string {
	status = dashboardText(status)
	if status == "in-use" {
		return "in use"
	}
	return status
}

func (m *dashboard) statusColor(status string) string {
	switch m.statusLabel(status) {
	case "dirty", "protected":
		return m.palette().warning
	case "leased":
		return m.palette().leased
	case "in use":
		return m.palette().inuse
	default:
		return m.palette().accent
	}
}

func (m *dashboard) pill(status string) string {
	label := m.statusLabel(status)
	if label == "" {
		return ""
	}
	if m.monochrome {
		return "[" + label + "]"
	}
	return m.ink(label, m.statusColor(status), false)
}

func (m *dashboard) row(r DashboardRow, width int, selected bool) string {
	annotation := strings.ReplaceAll(dashboardText(r.Annotation), "\n", " ")
	title := strings.ReplaceAll(dashboardText(r.Title), "\n", " ")
	prefix := "  "
	if selected {
		prefix = "> "
	}
	rightPlain := m.statusLabel(r.Status)
	if m.monochrome {
		rightPlain = m.pill(r.Status)
	}
	if annotation != "" {
		if rightPlain != "" {
			rightPlain = annotation + "  " + rightPlain
		} else {
			rightPlain = annotation
		}
	}
	titleWidth := max(1, width-lipgloss.Width(prefix)-lipgloss.Width(rightPlain)-1)
	title = clip(title, titleWidth)
	gap := max(1, width-lipgloss.Width(prefix)-lipgloss.Width(title)-lipgloss.Width(rightPlain))
	plain := clip(prefix+title+strings.Repeat(" ", gap)+rightPlain, width)
	if selected && !m.monochrome {
		return lipgloss.NewStyle().Background(lipgloss.Color(m.palette().selected)).Foreground(lipgloss.Color(m.palette().text)).Bold(true).Render(plain)
	}
	if m.monochrome {
		return plain
	}
	right := m.pill(r.Status)
	if annotation != "" && right != "" {
		right = m.hint(annotation) + "  " + right
	} else if annotation != "" {
		right = m.hint(annotation)
	}
	return clip(m.ink(prefix+title, m.palette().text, false)+strings.Repeat(" ", gap)+right, width)
}

func (m *dashboard) detailsText(row DashboardRow) string {
	details := dashboardText(row.Details)
	title := dashboardText(row.Title)
	target := dashboardText(row.Action.Target)
	lines := strings.Split(details, "\n")
	for len(lines) > 0 && (lines[0] == "" || lines[0] == title || lines[0] == target) {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

func (m *dashboard) listBody(width, height int) string {
	if height <= 0 {
		return ""
	}
	if m.loading {
		if _, ok := m.cache[m.page]; !ok {
			return padBlock(m.hint("Loading…"), width, height)
		}
	}
	if _, ok := m.cache[m.page]; !ok && m.err != "" && !m.loading {
		return padBlock(m.hint(window("Could not load. Press r to retry.", width, height, 0)), width, height)
	}
	rows := m.rows()
	if len(rows) == 0 {
		message := "No branches. Press n to start one."
		if m.options.JJ && m.page == HomePage {
			message = "No trees. Press n to start one."
		}
		if m.page == TreesPage {
			message = "No trees. Press n on home to create one."
		}
		if m.page == CleanupPage {
			message = "Nothing to clean up. Your work is kept."
		}
		if m.searching && m.input.Value() != "" {
			message = "No matches. Esc clears find."
		}
		return padBlock(m.hint(window(message, width, height, 0)), width, height)
	}
	selected := min(m.selected, len(rows)-1)
	start := max(0, selected-height+1)
	end := min(len(rows), start+height)
	var lines []string
	for i := start; i < end; i++ {
		lines = append(lines, m.row(rows[i], width, i == selected))
	}
	return padBlock(strings.Join(lines, "\n"), width, height)
}

func (m *dashboard) detailsBody(width, height int) string {
	if height <= 0 {
		return ""
	}
	rows := m.rows()
	if len(rows) == 0 {
		return padBlock("", width, height)
	}
	selected := min(m.selected, len(rows)-1)
	text := window(m.detailsText(rows[selected]), width, height, m.detailOffset)
	return padBlock(m.hint(text), width, height)
}

func (m *dashboard) hairline(title string, width int) string {
	prefix := "─ " + title + " "
	rest := max(0, width-lipgloss.Width(prefix))
	return m.hint(clip(prefix+strings.Repeat("─", rest), width))
}

func (m *dashboard) panel(title, body string, width, height, chrome int) string {
	if height <= 0 || width <= 0 {
		return ""
	}
	innerW, bodyH := width, height
	if chrome == 3 {
		innerW = max(1, width-2)
		bodyH = max(0, height-3)
	} else if chrome == 1 {
		bodyH = max(0, height-1)
	}
	body = padBlock(body, innerW, bodyH)
	switch chrome {
	case 3:
		inner := clip(m.hint(title), innerW)
		if bodyH > 0 {
			inner += "\n" + body
		}
		b := lipgloss.RoundedBorder()
		if m.monochrome {
			b = lipgloss.NormalBorder()
		}
		style := lipgloss.NewStyle().Border(b).Width(width).Height(height)
		if !m.monochrome {
			style = style.BorderForeground(lipgloss.Color(m.palette().border))
		}
		return fitBlock(style.Render(inner), width, height)
	case 1:
		block := m.hairline(title, width)
		if bodyH > 0 {
			block += "\n" + body
		}
		return fitBlock(block, width, height)
	default:
		return fitBlock(body, width, height)
	}
}

func (m *dashboard) listTitle() string {
	n := len(m.rows())
	name := "Branches"
	if m.page == TreesPage || (m.page == HomePage && m.options.JJ) {
		name = "Trees"
	}
	if m.page == CleanupPage {
		name = "Cleanup"
	}
	if m.creating || m.help {
		return name
	}
	return fmt.Sprintf("%s  %d", name, n)
}

func (m *dashboard) overlayTitle() string {
	if m.creating {
		return "New branch"
	}
	return "Keys"
}

func (m *dashboard) overlayBody(width, height int) string {
	if m.creating {
		body := "Branch name\n" + m.input.View()
		if m.validating {
			body += "\n" + m.hint("Checking branch name...")
		} else if m.err != "" {
			body += "\n" + m.ink(dashboardText(m.err), m.palette().warning, false)
		}
		return padBlock(body, width, height)
	}
	help := "Enter does the highlighted action.\nEsc goes back; q quits.\nn new branch; t trees; c cleanup.\nCleanup: Enter removes listed unused trees; Esc cancels.\n/ find; j/k move; PgUp/Dn scroll the Details panel."
	return padBlock(m.hint(window(help, width, height, m.detailOffset)), width, height)
}

func (m *dashboard) selectedRow() (DashboardRow, bool) {
	rows := m.rows()
	if len(rows) == 0 {
		return DashboardRow{}, false
	}
	return rows[min(m.selected, len(rows)-1)], true
}

func (m *dashboard) enterVerb() (key, label string) {
	if m.help {
		return "Esc", "close help"
	}
	if m.creating {
		return "Enter", "create this branch"
	}
	if m.page == CleanupPage {
		n := len(m.visible().CandidatePaths)
		if n > 0 {
			word := "tree"
			if n != 1 {
				word = "trees"
			}
			return "Enter", fmt.Sprintf("remove %d unused %s", n, word)
		}
		return "Esc", "back"
	}
	row, ok := m.selectedRow()
	if !ok {
		if m.page == TreesPage {
			if m.options.Page == HomePage {
				return "Esc", "back"
			}
			return "q", "quit"
		}
		return "n", "start a branch"
	}
	name := strings.ReplaceAll(dashboardText(row.Title), "\n", " ")
	switch row.Action.Kind {
	case OpenTree:
		if row.Action.Name != "" {
			name = "tree " + strings.ReplaceAll(dashboardText(row.Action.Name), "\n", " ")
		} else if row.Annotation != "" {
			name = strings.ReplaceAll(dashboardText(row.Annotation), "\n", " ")
		}
		return "Enter", "open " + name
	default:
		return "Enter", "resume " + name
	}
}

func (m *dashboard) footer(width int) string {
	key, label := m.enterVerb()
	label = clip(label, max(8, width-lipgloss.Width(key)-2))
	primary := m.primary(key) + "  " + m.hint(label)
	if m.page == CleanupPage && len(m.visible().CandidatePaths) > 0 && !m.help && !m.creating {
		primary = m.primary(key) + "  " + m.ink(label, m.palette().warning, false)
	}
	secondary := ""
	note := ""
	switch {
	case m.help:
		secondary = "Esc close  q quit"
	case m.creating:
		secondary = "Esc back"
	case m.searching:
		secondary = "Up/Down select  Esc clear  Ctrl+C quit"
	case m.page == CleanupPage:
		if len(m.visible().CandidatePaths) > 0 {
			note = "Git branches are kept."
			secondary = "Esc  cancel  q quit"
		} else {
			secondary = "Esc  back  q quit"
		}
	case m.page == TreesPage:
		secondary = "/ find  ? help  q quit"
	case m.options.JJ:
		secondary = "n new  c cleanup  / find  ? help  q quit"
	default:
		secondary = "n new  t trees  c cleanup  / find  ? help  q quit"
	}
	result := wrapped(primary, width)
	if note != "" {
		result += "\n" + m.hint(wrapped(note, width))
	}
	if secondary != "" {
		result += "\n" + m.hint(wrapped(m.shortcuts(secondary), width))
	}
	return result
}

func (m *dashboard) shortcuts(s string) string {
	var groups []string
	for _, part := range strings.Split(s, "  ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, label, ok := strings.Cut(part, " ")
		if !ok {
			groups = append(groups, m.primary(key))
			continue
		}
		groups = append(groups, m.primary(key)+" "+m.hint(label))
	}
	return strings.Join(groups, "  ")
}

type framePlan struct {
	contentWidth                                          int
	header, banner, list, details, overlay, footer, frame string
	headerH, bannerH, listH, detailsH, overlayH, footerH  int
	chrome                                                int
	inputX, inputY                                        int
	inputOnScreen                                         bool
}

func (m *dashboard) plan() framePlan {
	cw := m.contentWidth()
	p := framePlan{contentWidth: cw}
	dropUser := false
	dropOrphan := false
	headerLimit := 0
	footerLimit := 0
	chrome := 1
	if m.width >= 48 && m.height >= 16 {
		chrome = 3
	}
	detailsBody := 1
	if m.height >= 24 {
		detailsBody = 5
	} else if m.height >= 16 {
		detailsBody = 3
	}
	overlay := m.help || m.creating
collapse:
	for {
		header := m.headerBar(cw)
		if headerLimit > 0 {
			header = padBlock(header, cw, min(blockHeight(header), headerLimit))
		}
		kind := m.currentBanner(dropUser, dropOrphan)
		banner := m.bannerLine(cw, kind)
		footer := m.footer(cw)
		if footerLimit > 0 {
			footer = padBlock(footer, cw, min(blockHeight(footer), footerLimit))
		}
		headerH, bannerH, footerH := blockHeight(header), blockHeight(banner), blockHeight(footer)
		remaining := m.height - headerH - bannerH - footerH
		p.header, p.banner, p.footer = header, banner, footer
		p.headerH, p.bannerH, p.footerH = headerH, bannerH, footerH
		p.chrome = chrome
		if remaining < 0 {
			remaining = 0
		}
		if overlay {
			p.overlayH = remaining
			if headerH+bannerH+footerH+remaining <= m.height && remaining >= 1 {
				break
			}
		} else {
			detailChrome := chrome
			listChrome := chrome
			body := detailsBody
			if body == 0 {
				detailChrome = 0
			}
			detailPane := body + detailChrome
			if body == 0 {
				detailPane = 0
			}
			listPane := remaining - detailPane
			listBody := listPane - listChrome
			if listBody >= 2 && headerH+bannerH+footerH+listPane+detailPane <= m.height {
				p.listH, p.detailsH = listPane, detailPane
				break
			}
			p.listH, p.detailsH = max(0, listPane), max(0, detailPane)
		}
		switch {
		case chrome == 3:
			chrome = 1
		case detailsBody > 0 && !overlay:
			detailsBody = 0
		case !dropUser:
			dropUser = true
		case !dropOrphan:
			dropOrphan = true
		case footerLimit == 0:
			footerLimit = 2
		case headerLimit == 0:
			headerLimit = 2
		default:
			break collapse
		}
	}
	inner := func(h, chrome int) int {
		if chrome == 3 {
			return max(0, h-3)
		}
		if chrome == 1 {
			return max(0, h-1)
		}
		return max(0, h)
	}
	innerW := func(chrome int) int {
		if chrome == 3 {
			return max(1, cw-2)
		}
		return cw
	}
	if overlay {
		bodyW, bodyH := innerW(p.chrome), inner(p.overlayH, p.chrome)
		p.overlay = m.panel(m.overlayTitle(), m.overlayBody(bodyW, bodyH), cw, p.overlayH, p.chrome)
		p.inputX = 1
		if p.chrome == 3 {
			p.inputX++
		}
		p.inputY = p.headerH + p.bannerH
		if p.chrome == 3 {
			p.inputY += 2
		} else if p.chrome == 1 {
			p.inputY += 1
		}
		p.inputY++
	} else {
		listInnerW, listInnerH := innerW(p.chrome), inner(p.listH, p.chrome)
		detailInnerW, detailInnerH := innerW(p.chrome), inner(p.detailsH, p.chrome)
		p.list = m.panel(m.listTitle(), m.listBody(listInnerW, listInnerH), cw, p.listH, p.chrome)
		if p.detailsH > 0 {
			p.details = m.panel("Details", m.detailsBody(detailInnerW, detailInnerH), cw, p.detailsH, p.chrome)
		}
		p.inputX = 1
		p.inputY = p.headerH
	}
	if m.searching {
		p.inputX = 1 + lipgloss.Width("Find: ")
		p.inputY = p.headerH
	}
	p.inputOnScreen = p.inputY >= 0 && p.inputY < m.height && p.inputX >= 0 && p.inputX < m.width
	body := joinBlocks(p.header, p.banner, p.list, p.details, p.overlay)
	lines := []string{}
	if body != "" {
		lines = strings.Split(body, "\n")
	}
	footerLines := []string{}
	if p.footer != "" {
		footerLines = strings.Split(p.footer, "\n")
	}
	need := m.height - len(footerLines)
	if need < 0 {
		footerLines = footerLines[:m.height]
		need = 0
	}
	for len(lines) < need {
		lines = append(lines, "")
	}
	if len(lines) > need {
		lines = lines[:need]
	}
	lines = append(lines, footerLines...)
	for i := range lines {
		lines[i] = " " + lines[i]
		lines[i] = clip(lines[i], m.width)
	}
	p.frame = strings.Join(lines, "\n")
	if lipgloss.Height(p.frame) < m.height {
		p.frame = padBlock(p.frame, m.width, m.height)
	}
	return p
}

func (m *dashboard) View() tea.View {
	focused := m.input.Focused()
	m.input.SetVirtualCursor(false)
	p := m.plan()
	if focused && !p.inputOnScreen {
		m.input.SetVirtualCursor(true)
		p = m.plan()
	}
	v := tea.NewView(p.frame)
	v.AltScreen = true
	if focused && p.inputOnScreen {
		if c := m.input.Cursor(); c != nil {
			c.Position.X += p.inputX
			c.Position.Y += p.inputY
			v.Cursor = c
		}
	}
	return v
}
