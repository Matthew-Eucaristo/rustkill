// Package ui is the interactive Bubble Tea front end.
package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Matthew-Eucaristo/rustkill/internal/clean"
	"github.com/Matthew-Eucaristo/rustkill/internal/report"
	"github.com/Matthew-Eucaristo/rustkill/internal/scan"
)

type status int

const (
	idle status = iota
	queued
	working
	done
	failed
)

type groupBy int

const (
	byProject groupBy = iota
	byFramework
	byAge
)

func (g groupBy) String() string { return [...]string{"project", "framework", "age"}[g] }

type sortBy int

const (
	bySize sortBy = iota
	byIdle
	byName
)

func (s sortBy) String() string { return [...]string{"size", "idle longest", "name"}[s] }

// row is a group header (it == nil) or an item.
type row struct {
	group string
	title string
	sub   string // header detail: workspace, edition, toolchain, activity
	it    *scan.Item
}

func (r row) key() string {
	if r.it == nil {
		return "group:" + r.group
	}
	return r.it.ID
}

type (
	loadedMsg struct {
		res *scan.Result
		err error
	}
	measuredMsg scan.Measurement
	measureDone struct{}
	actedMsg    struct{ out *clean.Outcome }
)

// Model is the TUI state. All mutation happens in Update.
type Model struct {
	opts   scan.Options
	copts  clean.Options
	minAge time.Duration
	home   string

	res       *scan.Result
	err       error
	sizes     chan scan.Measurement
	measuring bool
	freeDisk  int64
	started   time.Time

	group     groupBy
	sort      sortBy
	filter    string
	filtering bool
	cursorKey string
	moved     bool // the user moved the cursor; until then it stays on top
	offset    int
	width     int
	height    int
	legend    bool

	selected map[string]bool
	status   map[string]status
	errs     map[string]string
	confirm  []string // item IDs awaiting y/n
	queue    []string
	quitWarn bool

	outcomes []*clean.Outcome
}

// New builds a model that scans with opts and acts through copts. With
// minAge, only projects idle at least that long are listed.
func New(opts scan.Options, copts clean.Options, minAge time.Duration) *Model {
	return &Model{
		opts: opts, copts: copts, minAge: minAge, home: opts.Home,
		selected: map[string]bool{}, status: map[string]status{}, errs: map[string]string{},
		width: 100, height: 30, freeDisk: -1,
	}
}

// Outcomes reports what was done once the program has exited.
func (m *Model) Outcomes() []*clean.Outcome { return m.outcomes }

func (m *Model) Init() tea.Cmd { return m.load() }

func (m *Model) load() tea.Cmd {
	opts, minAge := m.opts, m.minAge
	m.started = time.Now()
	return func() tea.Msg {
		res, err := scan.Scan(opts)
		if err == nil && minAge > 0 {
			var keep []*scan.Item
			for _, it := range res.Items {
				if p := it.Project; p == nil || (!p.LastActivity.IsZero() && time.Since(p.LastActivity) >= minAge) {
					keep = append(keep, it)
				}
			}
			res.Items = keep
		}
		return loadedMsg{res, err}
	}
}

func waitSize(ch chan scan.Measurement) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-ch
		if !ok {
			return measureDone{}
		}
		return measuredMsg(s)
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case loadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.res = msg.res
		m.freeDisk = m.diskFree()
		m.sizes = make(chan scan.Measurement)
		m.measuring = true
		go m.res.MeasureInto(m.sizes)
		if rows := m.rows(); len(rows) > 0 && m.cursorKey == "" {
			m.cursorKey = firstItem(rows)
		}
		return m, waitSize(m.sizes)
	case measuredMsg:
		if it := m.find(msg.ID); it != nil {
			it.Apply(scan.Measurement(msg))
			if !it.Safety.Level.Allowed() {
				delete(m.selected, it.ID)
			}
		}
		// Sizes re-sort the list; keep the first row in view until the
		// user starts moving.
		if rows := m.rows(); !m.moved && len(rows) > 0 {
			m.cursorKey, m.offset = firstItem(rows), 0
		}
		return m, waitSize(m.sizes)
	case measureDone:
		m.measuring = false
	case actedMsg:
		o := msg.out
		m.outcomes = append(m.outcomes, o)
		delete(m.selected, o.ID)
		if o.Status == clean.StatusDone || o.Status == clean.StatusDryRun {
			m.status[o.ID] = done
			delete(m.errs, o.ID)
		} else {
			m.status[o.ID] = failed
			m.errs[o.ID] = o.Error
		}
		m.freeDisk = m.diskFree()
		return m, m.next()
	case tea.KeyPressMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		if m.busy() && !m.quitWarn {
			m.quitWarn = true
			return nil
		}
		return tea.Quit
	}
	if m.legend {
		m.legend = false
		return nil
	}
	if m.confirm != nil {
		if k == "y" || k == "Y" {
			return m.start()
		}
		m.confirm = nil
		return nil
	}
	if m.filtering {
		switch k {
		case "enter", "esc":
			if k == "esc" {
				m.filter = ""
			}
			m.filtering = false
		case "backspace":
			if r := []rune(m.filter); len(r) > 0 {
				m.filter = string(r[:len(r)-1])
			}
		default:
			if msg.Text != "" {
				m.filter += msg.Text
			}
		}
		m.offset = 0
		return nil
	}
	if m.res == nil {
		if k == "q" || k == "esc" {
			return tea.Quit
		}
		return nil
	}
	rows := m.rows()
	i := m.cursor(rows)
	switch k {
	case "up", "k", "down", "j", "pgup", "ctrl+u", "pgdown", "ctrl+d", "home", "g", "end", "G":
		m.moved = true
	}
	switch k {
	case "q":
		if m.busy() {
			return nil
		}
		return tea.Quit
	case "esc":
		if m.filter != "" {
			m.filter = ""
			return nil
		}
		if m.busy() {
			return nil
		}
		return tea.Quit
	case "?":
		m.legend = true
		return nil
	case "/":
		m.filtering = true
		return nil
	case "up", "k":
		i--
	case "down", "j":
		i++
	case "pgup", "ctrl+u":
		i -= m.bodyHeight()
	case "pgdown", "ctrl+d":
		i += m.bodyHeight()
	case "home", "g":
		i = 0
	case "end", "G":
		i = len(rows) - 1
	case "tab":
		m.group = (m.group + 1) % 3
		m.top()
		return nil
	case "o":
		m.sort = (m.sort + 1) % 3
		m.top()
		return nil
	case "R", "ctrl+r":
		if m.busy() || m.measuring {
			return nil
		}
		m.res, m.status, m.errs, m.selected = nil, map[string]status{}, map[string]string{}, map[string]bool{}
		return m.load()
	case "space":
		if len(rows) > 0 {
			m.toggle(m.leaves(rows[i]))
		}
		return nil
	case "s":
		m.toggle(m.matching(func(it *scan.Item) bool { return bulk(it) && it.Category == scan.CatProject }))
		return nil
	case "a":
		m.toggle(m.matching(bulk))
		return nil
	case "enter", "d", "delete", "backspace":
		m.ask(rows, i)
		return nil
	default:
		return nil
	}
	if len(rows) > 0 {
		i = max(0, min(i, len(rows)-1))
		m.cursorKey = rows[i].key()
	}
	return nil
}

// bulk: what s and a select: safe items that hold something.
func bulk(it *scan.Item) bool { return it.Safety.Level == scan.Safe && it.Size.Allocated > 0 }

// top puts the cursor on the first item after the order changed, unless the
// user has moved it.
func (m *Model) top() {
	m.offset = 0
	if rows := m.rows(); !m.moved && len(rows) > 0 {
		m.cursorKey = firstItem(rows)
	}
}

// selectable: measured, allowed, not done.
func (m *Model) selectable(it *scan.Item) bool {
	s := m.status[it.ID]
	return it.Measured && it.Safety.Level.Allowed() && s != done && s != working && s != queued
}

// matching lists the visible selectable items that pass f.
func (m *Model) matching(f func(*scan.Item) bool) []string {
	var ids []string
	for _, r := range m.rows() {
		if r.it != nil && m.selectable(r.it) && f(r.it) {
			ids = append(ids, r.it.ID)
		}
	}
	return ids
}

// leaves are the selectable item IDs a row stands for.
func (m *Model) leaves(r row) []string {
	if r.it != nil {
		if m.selectable(r.it) {
			return []string{r.it.ID}
		}
		return nil
	}
	var ids []string
	for _, x := range m.rows() {
		if x.it != nil && x.group == r.group && m.selectable(x.it) {
			ids = append(ids, x.it.ID)
		}
	}
	return ids
}

func (m *Model) toggle(ids []string) {
	all := len(ids) > 0
	for _, id := range ids {
		all = all && m.selected[id]
	}
	for _, id := range ids {
		if all {
			delete(m.selected, id)
		} else {
			m.selected[id] = true
		}
	}
}

// ask targets the selection, or the item under the cursor.
func (m *Model) ask(rows []row, i int) {
	var ids []string
	for _, r := range rows {
		if r.it != nil && m.selected[r.it.ID] {
			ids = append(ids, r.it.ID)
		}
	}
	if len(ids) == 0 && len(rows) > 0 {
		if r := rows[i]; r.it != nil {
			ids = []string{r.it.ID}
		} else {
			ids = m.leaves(r)
		}
	}
	ids = slices.DeleteFunc(ids, func(id string) bool { s := m.status[id]; return s == done || s == working || s == queued })
	if len(ids) > 0 {
		m.confirm = ids
	}
}

// plan splits the confirmation into items that will run and those refused.
func (m *Model) plan() (run, refuse []*scan.Item) {
	for _, id := range m.confirm {
		it := m.find(id)
		switch {
		case it == nil:
		case it.Measured && it.Safety.Level.Allowed():
			run = append(run, it)
		default:
			refuse = append(refuse, it)
		}
	}
	return run, refuse
}

func (m *Model) start() tea.Cmd {
	run, _ := m.plan()
	for _, it := range run {
		m.status[it.ID] = queued
		m.queue = append(m.queue, it.ID)
	}
	m.confirm = nil
	if m.working() {
		return nil
	}
	return m.next()
}

// next runs queued items one at a time, so free-space measurements and
// rustup and cargo commands never overlap.
func (m *Model) next() tea.Cmd {
	if len(m.queue) == 0 {
		m.quitWarn = false
		return nil
	}
	id := m.queue[0]
	m.queue = m.queue[1:]
	it := m.find(id)
	if it == nil {
		return m.next()
	}
	m.status[id] = working
	opts := m.copts
	return func() tea.Msg { return actedMsg{clean.Act(it, it.Fingerprint, opts)} }
}

func (m *Model) busy() bool { return m.working() || len(m.queue) > 0 }

func (m *Model) working() bool {
	for _, s := range m.status {
		if s == working {
			return true
		}
	}
	return false
}

func (m *Model) find(id string) *scan.Item {
	if m.res == nil {
		return nil
	}
	for _, it := range m.res.Items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

func (m *Model) visible(it *scan.Item) bool {
	if m.filter == "" {
		return true
	}
	f := strings.ToLower(m.filter)
	hay := strings.ToLower(it.Path + " " + it.Name + " " + it.Kind + " " + it.Tool)
	if p := it.Project; p != nil {
		hay += strings.ToLower(" " + p.Name + " " + p.Toolchain + " " + strings.Join(p.Tools, " "))
	}
	return strings.Contains(hay, f)
}

// ageBucket groups projects by how long they have been idle.
func ageBucket(p *scan.Project) (string, string) {
	if p.LastActivity.IsZero() {
		return "5", "activity unknown"
	}
	d := time.Since(p.LastActivity)
	switch {
	case d >= 90*24*time.Hour:
		return "1", "idle 90+ days"
	case d >= 30*24*time.Hour:
		return "2", "idle 30–90 days"
	case d >= 7*24*time.Hour:
		return "3", "idle 7–30 days"
	}
	return "4", "active this week"
}

// rows lists groups and their items in the current order.
func (m *Model) rows() []row {
	if m.res == nil {
		return nil
	}
	type grp struct {
		key, title, sub string
		rank            int
		items           []*scan.Item
		bytes           int64
	}
	groups := map[string]*grp{}
	var order []*grp
	for _, it := range m.res.Items {
		if !m.visible(it) {
			continue
		}
		key, title, sub, rank := string(it.Category), report.CategoryTitle(it.Category), "", 10
		switch it.Category {
		case scan.CatCache:
			rank = 20
		case scan.CatGlobal:
			rank = 30
		case scan.CatRuntime:
			rank = 40
		}
		if p := it.Project; p != nil {
			switch m.group {
			case byProject:
				key, title, sub = "p:"+p.Path, m.tilde(p.Path), report.ProjectBadges(p, time.Now())
			case byFramework:
				key, title = "f:"+p.Framework(), p.Framework()
			case byAge:
				b, t := ageBucket(p)
				key, title = "a:"+b, t
				rank = 10 + int(b[0]-'0')
			}
		}
		g := groups[key]
		if g == nil {
			g = &grp{key: key, title: title, sub: sub, rank: rank}
			groups[key] = g
			order = append(order, g)
		}
		g.items = append(g.items, it)
		g.bytes += max(it.Size.Allocated, 0)
	}
	slices.SortStableFunc(order, func(a, b *grp) int {
		if a.rank != b.rank {
			return cmp.Compare(a.rank, b.rank)
		}
		if m.group == byAge || m.sort == byName {
			return cmp.Compare(strings.ToLower(a.title), strings.ToLower(b.title))
		}
		return cmp.Compare(b.bytes, a.bytes)
	})
	var rows []row
	for _, g := range order {
		slices.SortStableFunc(g.items, func(a, b *scan.Item) int {
			switch m.sort {
			case byIdle:
				return lastUse(a).Compare(lastUse(b))
			case byName:
				return cmp.Compare(a.Path+a.Name, b.Path+b.Name)
			}
			return cmp.Compare(b.Size.Allocated, a.Size.Allocated)
		})
		sub := g.sub
		if sub == "" {
			sub = fmt.Sprintf("%d item(s)", len(g.items))
		}
		rows = append(rows, row{group: g.key, title: g.title, sub: sub})
		for _, it := range g.items {
			rows = append(rows, row{group: g.key, title: g.title, it: it})
		}
	}
	return rows
}

func lastUse(it *scan.Item) time.Time {
	if it.Project != nil {
		return it.Project.LastActivity
	}
	return it.LastUsed
}

func firstItem(rows []row) string {
	for _, r := range rows {
		if r.it != nil {
			return r.key()
		}
	}
	return rows[0].key()
}

func (m *Model) cursor(rows []row) int {
	for i, r := range rows {
		if r.key() == m.cursorKey {
			return i
		}
	}
	return 0
}

func (m *Model) diskFree() int64 {
	if len(m.opts.Roots) == 0 {
		return -1
	}
	return clean.FreeBytes(m.opts.Roots[0])
}
