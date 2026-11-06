package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Matthew-Eucaristo/rustkill/internal/clean"
	"github.com/Matthew-Eucaristo/rustkill/internal/format"
	"github.com/Matthew-Eucaristo/rustkill/internal/report"
	"github.com/Matthew-Eucaristo/rustkill/internal/scan"
)

// Plain ANSI colors so the terminal theme decides the actual shades.
var (
	accent  = lipgloss.Color("2")
	brand   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(accent).Padding(0, 1)
	bold    = lipgloss.NewStyle().Bold(true)
	faint   = lipgloss.NewStyle().Faint(true)
	cyan    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	green   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellow  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	red     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	magenta = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	struck  = lipgloss.NewStyle().Faint(true).Strikethrough(true)
	heading = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	banner  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("1")).Padding(0, 1)
	cursorC = green.Render("▌")
)

const (
	barWidth   = 14
	detailRows = 5
	// header, blank, column header, separator + detail, summary, help
	chromeLines = 6 + detailRows
)

func (m *Model) bodyHeight() int { return max(3, m.height-chromeLines) }

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "rustkill"
	return v
}

func (m *Model) tilde(p string) string { return format.Tilde(p, m.home) }

func (m *Model) render() string {
	if m.err != nil {
		return red.Render("rustkill: "+m.err.Error()) + "\n\n" + faint.Render("q quit")
	}
	if m.res == nil {
		return m.header() + "\n\n  " + faint.Render("Scanning "+m.rootsText()+" for Rust projects, Cargo caches and toolchains …")
	}
	var b strings.Builder
	line := func(s string) { b.WriteString(ansi.Truncate(s, m.width, "…") + "\n") }
	line(m.header())
	line(m.warningLine())
	rows := m.rows()
	cur := m.cursor(rows)
	body := m.bodyHeight()
	full := m.legend || m.confirm != nil
	if full {
		body += 1 + detailRows // the detail pane is not shown
	}
	var lines []string
	switch {
	case m.legend:
		lines = m.legendLines()
	case m.confirm != nil:
		lines = m.confirmLines(body)
	case len(rows) == 0 && m.filter != "":
		lines = []string{"", "  " + faint.Render("Nothing matches “"+m.filter+"”. esc clears the filter.")}
	case len(rows) == 0:
		lines = []string{"", "  " + faint.Render("Nothing to clean in "+m.rootsText()+"."), "",
			"  " + faint.Render("rustkill looks for Cargo.toml projects and the target folders cargo builds into,"),
			"  " + faint.Render("plus Cargo's caches, tools from cargo install and rustup's toolchains.")}
	default:
		l := m.layout(rows)
		line(faint.Render("    " + l.columns("", "SIZE", "FREES", fmt.Sprintf("%-5s", "IDLE"), fmt.Sprintf("%-11s", "SAFETY")) +
			"ITEM  (by " + m.group.String() + ", " + m.sort.String() + ")"))
		body--
		if cur < m.offset {
			m.offset = cur
		}
		if cur >= m.offset+body {
			m.offset = cur - body + 1
		}
		m.offset = max(0, min(m.offset, max(0, len(rows)-body)))
		largest := int64(1)
		for _, r := range rows {
			if r.it != nil {
				largest = max(largest, r.it.Size.Allocated)
			}
		}
		for i := m.offset; i < min(len(rows), m.offset+body); i++ {
			lines = append(lines, m.row(rows[i], i == cur, largest, l))
		}
	}
	for i := range body {
		if i < len(lines) {
			line(lines[i])
		} else {
			line("")
		}
	}
	if !full {
		line(faint.Render(strings.Repeat("─", max(0, m.width))))
		var detail []string
		if len(rows) > 0 {
			detail = m.detail(rows[cur])
		}
		for i := range detailRows {
			if i < len(detail) {
				line(detail[i])
			} else {
				line("")
			}
		}
	}
	line(m.summary())
	b.WriteString(ansi.Truncate(m.help(), m.width, "…"))
	return b.String()
}

func (m *Model) rootsText() string {
	var rs []string
	for _, r := range m.opts.Roots {
		rs = append(rs, m.tilde(r))
	}
	return strings.Join(rs, ", ")
}

// header is the mode, the totals and the roots, dropping the least
// important parts when the terminal is narrow.
func (m *Model) header() string {
	mode := green.Render("● trash")
	switch m.copts.Mode {
	case clean.DryRun:
		mode = yellow.Render("● dry run")
	case clean.Permanent:
		mode = banner.Render("PERMANENT DELETE")
	}
	type part struct {
		text string
		drop int // dropped first when it does not fit: higher goes first
	}
	parts := []part{{brand.Render("rustkill") + " " + mode, 0}}
	if m.res != nil {
		var total, safe int64
		var free int64
		for _, it := range m.res.Items {
			if m.status[it.ID] == done || !it.Measured {
				continue
			}
			total += it.Size.Allocated
			if it.Safety.Level.Allowed() {
				free += it.Size.Exclusive()
			}
			if it.Safety.Level == scan.Safe {
				safe += it.Size.Exclusive()
			}
		}
		parts = append(parts, part{bold.Render(format.Bytes(total)) + " on disk", 3},
			part{cyan.Render(format.Bytes(free)) + " freeable", 1}, part{green.Render(format.Bytes(safe)) + " safe", 1})
		if m.freeDisk >= 0 {
			parts = append(parts, part{format.Bytes(m.freeDisk) + " free", 4})
		}
		if m.measuring {
			parts = append(parts, part{faint.Render("measuring…"), 2})
		}
	}
	if m.minAge > 0 {
		parts = append(parts, part{faint.Render("idle ≥ " + format.Ago(time.Now().Add(-m.minAge), time.Now())), 2})
	}
	parts = append(parts, part{faint.Render(m.rootsText()), 5})
	for drop := 6; ; drop-- {
		for _, sep := range []string{"  ·  ", " · "} {
			var keep []string
			for _, p := range parts {
				if p.drop < drop {
					keep = append(keep, p.text)
				}
			}
			s := " " + strings.Join(keep, faint.Render(sep))
			if lipgloss.Width(s) <= m.width || drop == 1 {
				return s
			}
		}
	}
}

// warningLine shows the first scan warning under the header.
func (m *Model) warningLine() string {
	if m.res == nil || len(m.res.Warnings) == 0 {
		return ""
	}
	w := "  ⚠ " + m.res.Warnings[0]
	if n := len(m.res.Warnings) - 1; n > 0 {
		w += fmt.Sprintf("  (+%d more: ? shows all)", n)
	}
	return yellow.Render(w)
}

// layout is the column set that fits the terminal: the size bar narrows and
// then goes, and FREES shows only when something is shared.
type layout struct {
	bar   int
	frees bool
}

func (m *Model) layout(rows []row) layout {
	l := layout{bar: barWidth}
	switch {
	case m.width < 70:
		l.bar = 0
	case m.width < 100:
		l.bar = 6
	case m.width < 120:
		l.bar = 10
	}
	for _, r := range rows {
		if r.it != nil && frees(r.it) != "" {
			l.frees = true
			break
		}
	}
	return l
}

// columns joins the fixed columns of a row in layout order.
func (l layout) columns(bar, size, free, idle, badge string) string {
	var b strings.Builder
	if l.bar > 0 {
		b.WriteString(bar + strings.Repeat(" ", max(0, l.bar-lipgloss.Width(bar))) + "  ")
	}
	b.WriteString(fmt.Sprintf("%9s", size) + "  ")
	if l.frees {
		b.WriteString(cyan.Render(fmt.Sprintf("%9s", free)) + "  ")
	}
	b.WriteString(idle + "  " + badge + " ")
	return b.String()
}

// frees is what removing the item frees now, shown when much of it is
// shared with something outside it.
func frees(it *scan.Item) string {
	if it.Measured && it.Size.Shared > 0 && it.Size.Exclusive() < it.Size.Allocated*9/10 {
		return format.Bytes(it.Size.Exclusive())
	}
	return ""
}

func (m *Model) row(r row, isCursor bool, largest int64, l layout) string {
	cur := " "
	if isCursor {
		cur = cursorC
	}
	if r.it == nil {
		var size int64
		for _, x := range m.rows() {
			if x.it != nil && x.group == r.group && m.status[x.it.ID] != done {
				size += max(x.it.Size.Allocated, 0)
			}
		}
		return fmt.Sprintf("%s %s %s  %s  %s", cur, m.check(m.leaves(r), r), heading.Render(r.title),
			bold.Render(format.Bytes(size)), faint.Render(r.sub))
	}
	it := r.it
	size := format.Bytes(-1)
	if it.Measured {
		size = format.Bytes(it.Size.Allocated)
	}
	cols := l.columns(m.bar(it, largest, l.bar), size, frees(it), m.idle(it), badge(it.Safety.Level))
	room := m.width - 4 - lipgloss.Width(cols) - 1
	line := cols + bold.Render(leftTrim(m.label(it), max(room, 8))) + "  " + m.flags(it)
	switch m.status[it.ID] {
	case done:
		line = struck.Render(ansi.Strip(line))
	case working, queued:
		line = faint.Render(ansi.Strip(line))
	case failed:
		line = red.Render(ansi.Strip(line))
	}
	return fmt.Sprintf("%s %s %s", cur, m.check([]string{it.ID}, r), line)
}

// leftTrim keeps the end of s, where a path names the folder, when s is
// wider than n.
func leftTrim(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return "…" + string(r[len(r)-n+1:])
	}
	return s
}

func (m *Model) label(it *scan.Item) string {
	if it.Project != nil && m.group != byProject {
		return m.tilde(it.Path)
	}
	return report.Label(it, m.home)
}

// bar: length is the size relative to the largest item, in eighth blocks;
// the part removal frees now is in the safety colour, shared bytes cyan.
func (m *Model) bar(it *scan.Item, largest int64, width int) string {
	if width <= 0 {
		return ""
	}
	t := it.Size.Allocated
	if !it.Measured || t <= 0 {
		return faint.Render(strings.Repeat("·", width))
	}
	cells := []rune(fine(t, largest, width))
	own := int(int64(len(cells)) * it.Size.Exclusive() / t)
	return levelStyle(it.Safety.Level).Render(string(cells[:own])) + cyan.Render(string(cells[own:])) +
		faint.Render(strings.Repeat("·", width-len(cells)))
}

var eighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

func fine(n, total int64, width int) string {
	if total <= 0 || n <= 0 {
		return ""
	}
	units := int(n * int64(width) * 8 / total)
	units = max(1, min(units, width*8))
	return strings.Repeat("█", units/8) + eighths[units%8]
}

func levelStyle(l scan.Level) lipgloss.Style {
	switch l {
	case scan.Safe:
		return green
	case scan.Review:
		return yellow
	case scan.Protected:
		return red
	}
	return magenta
}

func badge(l scan.Level) string {
	icon := map[scan.Level]string{scan.Safe: "●", scan.Review: "▲", scan.Protected: "■", scan.Live: "◉"}[l]
	return levelStyle(l).Render(fmt.Sprintf("%s %-9s", icon, l))
}

func (m *Model) idle(it *scan.Item) string {
	t := lastUse(it)
	ago := strings.TrimSuffix(format.Ago(t, time.Now()), " ago")
	if t.IsZero() {
		ago = "—"
	}
	s := fmt.Sprintf("%-5s", ago)
	switch d := time.Since(t); {
	case t.IsZero():
		return faint.Render(s)
	case d < 7*24*time.Hour:
		return green.Render(s)
	case d >= 90*24*time.Hour:
		return yellow.Render(s)
	}
	return s
}

func (m *Model) flags(it *scan.Item) string {
	var parts []string
	for _, r := range it.Safety.Reasons {
		parts = append(parts, levelStyle(r.Level).Render(codeLabel(r.Code)))
	}
	if len(it.Command) > 0 {
		parts = append(parts, magenta.Render("via "+report.CommandText(it)))
	}
	if e := m.errs[it.ID]; e != "" {
		parts = append(parts, red.Render("✗ "+e))
	}
	return strings.Join(parts, " ")
}

// codeLabel is a reason code as a short tag.
func codeLabel(c string) string {
	switch c {
	case scan.ReasonCatalogReview:
		return "check first"
	case scan.ReasonOldCargo:
		return "old cargo uses it"
	}
	return strings.ReplaceAll(c, "_", " ")
}

func (m *Model) check(ids []string, r row) string {
	if r.it != nil {
		switch s := m.status[r.it.ID]; {
		case s == done:
			return green.Render("✓")
		case s == working || s == queued:
			return faint.Render("◌")
		case !r.it.Safety.Level.Allowed():
			return faint.Render(" ")
		}
	}
	sel := 0
	for _, id := range ids {
		if m.selected[id] {
			sel++
		}
	}
	switch {
	case sel > 0 && sel == len(ids):
		return cyan.Render("●")
	case sel > 0:
		return cyan.Render("◐")
	}
	return faint.Render("○")
}

// detail explains the row under the cursor: what it is, how it comes back,
// why it has its level, and what exactly would run.
func (m *Model) detail(r row) []string {
	pad := "  "
	if r.it == nil {
		return []string{pad + heading.Render(r.title) + "  " + faint.Render(r.sub),
			pad + faint.Render("space selects every allowed item in the group · enter cleans the selection")}
	}
	it := r.it
	var out []string
	head := bold.Render(m.tilde(it.Path))
	if it.Path == "" {
		head = bold.Render(it.Name)
	}
	if it.Measured {
		head += "  " + faint.Render(format.Bytes(it.Size.Allocated)+" on disk")
		if it.Size.Shared > 0 {
			head += faint.Render(", frees "+format.Bytes(it.Size.Exclusive())+" now; ") + cyan.Render(format.Bytes(it.Size.Shared)+" shared")
		}
	}
	out = append(out, pad+head)
	out = append(out, pad+it.Explain)
	how := "Action: move to the " + clean.TrashName
	if m.copts.Mode == clean.Permanent {
		how = red.Render("Action: delete permanently")
	}
	if len(it.Command) > 0 {
		how = "Action: " + magenta.Render(report.CommandText(it)) + faint.Render(" (the tool's own cleanup; deletes permanently)")
	}
	if !it.Safety.Level.Allowed() {
		how = levelStyle(it.Safety.Level).Render("rustkill leaves this alone")
		if it.Note != "" && it.Safety.Has(scan.ReasonNotInstalled) {
			how += faint.Render(": " + it.Note)
		}
	}
	if it.Regenerate != "" {
		how += faint.Render("  ·  comes back with: ") + it.Regenerate
	}
	out = append(out, pad+how)
	var why []string
	for _, rs := range it.Safety.Reasons {
		if rs.Detail != it.Explain {
			why = append(why, levelStyle(rs.Level).Render(rs.Detail))
		}
	}
	if it.Note != "" {
		why = append(why, faint.Render(it.Note))
	}
	if !it.LastUsed.IsZero() {
		why = append(why, faint.Render("last used "+format.Ago(it.LastUsed, time.Now())+" ("+it.LastUsedSource+")"))
	}
	if len(why) == 0 {
		why = append(why, green.Render("rebuildable, ignored by git, not in use"))
	}
	out = append(out, pad+strings.Join(why, faint.Render(" · ")))
	if e := m.errs[it.ID]; e != "" {
		out = append(out, pad+red.Render(e))
	}
	for i := range out {
		out[i] = ansi.Truncate(out[i], m.width-1, "…")
	}
	return out
}

func (m *Model) summary() string {
	var sel int
	var bytes int64
	for id := range m.selected {
		if it := m.find(id); it != nil {
			sel++
			bytes += it.Size.Exclusive()
		}
	}
	parts := []string{fmt.Sprintf("%d selected · frees %s", sel, format.Bytes(bytes))}
	var trashed, byTool, deleted, freed int64
	var bad int
	for _, o := range m.outcomes {
		switch o.Status {
		case clean.StatusDone, clean.StatusDryRun:
			switch o.Action {
			case "command":
				byTool += o.Estimate
			case "delete":
				deleted += o.Estimate
			default:
				trashed += o.Estimate
			}
		default:
			bad++
		}
		if o.Freed > 0 {
			freed += o.Freed
		}
	}
	if m.copts.Mode == clean.DryRun && trashed+byTool > 0 {
		parts = append(parts, yellow.Render("would free "+format.Bytes(trashed+byTool)))
	} else {
		if trashed > 0 {
			parts = append(parts, green.Render("moved to "+clean.TrashName+" "+format.Bytes(trashed)))
		}
		if byTool > 0 {
			parts = append(parts, green.Render("freed by tools "+format.Bytes(byTool)))
		}
		if deleted > 0 {
			parts = append(parts, red.Render("deleted "+format.Bytes(deleted)))
		}
		if len(m.outcomes) > bad {
			parts = append(parts, bold.Render("free space "+signed(freed))+faint.Render(" measured"))
		}
	}
	if bad > 0 {
		parts = append(parts, red.Render(fmt.Sprintf("%d refused/failed", bad)))
	}
	if len(m.queue) > 0 {
		parts = append(parts, faint.Render(fmt.Sprintf("%d queued", len(m.queue))))
	}
	if m.filter != "" || m.filtering {
		f := "/" + m.filter
		if m.filtering {
			f += "▏"
		}
		parts = append(parts, cyan.Render(f))
	}
	return "  " + strings.Join(parts, faint.Render("  ·  "))
}

func signed(n int64) string {
	if n > 0 {
		return "+" + format.Bytes(n)
	}
	return format.Bytes(max(n, 0))
}

func (m *Model) help() string {
	switch {
	case m.legend:
		return faint.Render("  any key closes")
	case m.confirm != nil:
		if run, _ := m.plan(); len(run) == 0 {
			return faint.Render("  Nothing here can be cleaned. Any key closes")
		}
		return yellow.Render("  Proceed?") + "  " + bold.Render("y") + faint.Render(" yes   any other key cancels")
	case m.filtering:
		return faint.Render("  type to filter  enter keep  esc clear")
	case m.quitWarn:
		return yellow.Render("  Actions still running. Press ctrl+c again to quit anyway, or wait.")
	}
	return m.keyHints()
}

// keyHints lists the keys, dropping the least needed ones when the
// terminal is narrow; ? always stays and shows them all.
func (m *Model) keyHints() string {
	keys := []struct {
		text string
		drop int
	}{
		{"↑↓ move", 5}, {"space select", 0}, {"s safe in projects", 1}, {"a all safe", 3}, {"enter clean", 0},
		{"/ filter", 4}, {"tab group", 2}, {"o sort", 4}, {"R rescan", 6}, {"? legend", 0}, {"q quit", 0},
	}
	for drop := 7; ; drop-- {
		var keep []string
		for _, k := range keys {
			if k.drop < drop {
				keep = append(keep, k.text)
			}
		}
		if s := "  " + strings.Join(keep, "  "); lipgloss.Width(s) <= m.width || drop == 1 {
			return faint.Render(s)
		}
	}
}

// confirmLines lists exactly what will happen, split by how it frees space.
func (m *Model) confirmLines(height int) []string {
	run, refuse := m.plan()
	var trash, cmds []*scan.Item
	var tb, cb int64
	for _, it := range run {
		if len(it.Command) > 0 {
			cmds, cb = append(cmds, it), cb+it.Size.Exclusive()
		} else {
			trash, tb = append(trash, it), tb+it.Size.Exclusive()
		}
	}
	// heading puts the total at the right edge of a line that fits.
	heading := func(st lipgloss.Style, text string, n int64) string {
		w := min(m.width, 100) - 2
		total := format.Bytes(n)
		return st.Render(text + strings.Repeat(" ", max(2, w-lipgloss.Width(text)-len(total))) + total)
	}
	lines := []string{""}
	if m.copts.Mode == clean.DryRun {
		lines = append(lines, yellow.Render("  Dry run: nothing will change."), "")
	}
	room := height - 6
	list := func(items []*scan.Item, show func(*scan.Item) string) {
		for i, it := range items {
			if i >= room/2 && len(items) > room/2 {
				lines = append(lines, faint.Render(fmt.Sprintf("      … and %d more", len(items)-i)))
				return
			}
			lines = append(lines, fmt.Sprintf("      %9s  %s", format.Bytes(it.Size.Exclusive()), show(it)))
		}
	}
	if len(trash) > 0 {
		h := fmt.Sprintf("  Move to the %s · recoverable", clean.TrashName)
		st := green
		if m.copts.Mode == clean.Permanent {
			h, st = "  Delete permanently · NOT recoverable", red
		}
		lines = append(lines, heading(st, h, tb))
		list(trash, func(it *scan.Item) string { return m.tilde(it.Path) })
	}
	if len(cmds) > 0 {
		lines = append(lines, "", heading(magenta, "  Official commands · the tool deletes them", cb))
		list(cmds, func(it *scan.Item) string { return report.CommandText(it) })
	}
	if len(refuse) > 0 {
		lines = append(lines, "", red.Render(fmt.Sprintf("  Not included (%d):", len(refuse))))
		for i, it := range refuse {
			if len(lines) >= height-1 {
				lines = append(lines, faint.Render(fmt.Sprintf("      … and %d more", len(refuse)-i)))
				break
			}
			why := "not measured yet"
			if len(it.Safety.Reasons) > 0 {
				why = it.Safety.Reasons[0].Detail
			}
			lines = append(lines, fmt.Sprintf("      %s  %s", m.label(it), faint.Render(why)))
		}
	}
	return lines
}

func (m *Model) legendLines() []string {
	var out []string
	para := func(indent, text string) {
		w := max(20, m.width-len(indent)-1)
		for _, l := range strings.Split(ansi.Wrap(text, w, ""), "\n") {
			out = append(out, indent+l)
		}
	}
	out = append(out, "", "  "+bold.Render("Safety"))
	para("    ", badge(scan.Safe)+"  rebuildable and idle; "+bold.Render("s")+" selects these in projects, "+bold.Render("a")+" everywhere")
	para("    ", badge(scan.Review)+"  allowed, but worth a look: recent project or build, a shared target, installers you built, worktree, costs a download or reinstall")
	para("    ", badge(scan.Protected)+"  shown, never removed: git tracks it or does not ignore it, the default toolchain, or its tool is missing")
	para("    ", badge(scan.Live)+"  cargo, rustc or rust-analyzer is using it right now, or holds its build lock")
	out = append(out, "")
	para("  ", "Moving to the "+clean.TrashName+" frees space when you empty it; official commands free it right away.")
	out = append(out, "")
	para("  ", bold.Render("SIZE")+" is on disk. "+cyan.Render("FREES")+" appears when much of it is shared (APFS clones, hard links) and is what removing it frees now.")
	para("  ", "Folders (target, Cargo's caches) move to the "+clean.TrashName+". Toolchains, components and installed tools are removed by their own tool ("+magenta.Render("rustup toolchain uninstall, rustup component remove, cargo uninstall")+"). Free space is measured from the disk, not assumed.")
	out = append(out, "")
	para("  ", bold.Render("Keys")+"  ↑↓/jk move · space select · s safe in projects · a all safe · enter clean (asks first) · / filter · tab group by project, framework or idle time · o sort by size, idle time or name · R rescan · q quit")
	if m.res != nil && len(m.res.Warnings) > 0 {
		out = append(out, "", "  "+yellow.Render("Warnings"))
		for _, w := range m.res.Warnings {
			para("    ", yellow.Render("⚠ "+w))
		}
	}
	return out
}
