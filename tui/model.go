// Package tui is the interactive pager: a viewport over rendered markdown
// with search, live reload, clickable links, and a navigation stack.
package tui

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"mdview/render"
)

const (
	marginX   = 1 // keeps borders and rules off the terminal edge
	pollEvery = 500 * time.Millisecond
	closeW    = 3 // width of the " ✕ " button in the header
)

type keymap struct {
	Quit, Back, Top, Bottom, Search, Next, Prev, Reload, Cancel, Accept key.Binding
	NextLink, PrevLink, Follow, Copy                                    key.Binding
}

var keys = keymap{
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c")),
	Back:   key.NewBinding(key.WithKeys("esc", "backspace")),
	Top:    key.NewBinding(key.WithKeys("g", "home")),
	Bottom: key.NewBinding(key.WithKeys("G", "end")),
	Search: key.NewBinding(key.WithKeys("/")),
	Next:   key.NewBinding(key.WithKeys("n")),
	Prev:   key.NewBinding(key.WithKeys("N")),
	Reload: key.NewBinding(key.WithKeys("r")),
	Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c")),
	Accept: key.NewBinding(key.WithKeys("enter")),

	NextLink: key.NewBinding(key.WithKeys("tab")),
	PrevLink: key.NewBinding(key.WithKeys("shift+tab")),
	Follow:   key.NewBinding(key.WithKeys("enter")),
	Copy:     key.NewBinding(key.WithKeys("y")),
}

type tickMsg time.Time

// doc is one document on the navigation stack, with its view state.
type doc struct {
	path     string // "" when reading from stdin
	src      []byte
	lastMod  time.Time
	yOffset  int
	query    string
	matches  []int
	matchIdx int
}

func (d doc) name() string {
	if d.path == "" {
		return "stdin"
	}
	return filepath.Base(d.path)
}

type Model struct {
	renderer *render.Renderer
	maxWidth int
	// Open opens a URL or non-markdown file outside the viewer.
	Open func(target string) error
	// Copy puts text on the clipboard; nil means defaultCopy (then OSC 52).
	Copy func(text string) error

	cur   doc
	stack []doc // documents to return to; last is the most recent

	vp       viewport.Model
	ready    bool
	termW    int
	termH    int
	rendered render.Doc
	lines    []string // rendered, margin-padded lines
	plain    []string // ANSI-stripped rendered lines, for search

	search    textinput.Model
	searching bool
	notice    string

	links       []linkRef // every link in the document, reading order
	hover       *linkRef  // link under the mouse pointer
	focus       int       // keyboard-focused link (index into links), -1 none
	sel         selection
	lastClick   time.Time
	lastClickAt pos
	menu        *menu
	osc52       string // one-shot clipboard escape, emitted with the next frame
}

func New(path string, src []byte, r *render.Renderer, maxWidth int) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 200
	return Model{
		renderer: r,
		maxWidth: maxWidth,
		Open:     xdgOpen,
		cur:      newDoc(path, src),
		search:   ti,
		focus:    -1,
	}
}

func newDoc(path string, src []byte) doc {
	d := doc{path: path, src: src}
	if path != "" {
		if fi, err := os.Stat(path); err == nil {
			d.lastMod = fi.ModTime()
		}
	}
	return d
}

func xdgOpen(target string) error {
	cmd := exec.Command("xdg-open", target)
	cmd.Stdout, cmd.Stderr = nil, nil
	return cmd.Start()
}

func (m Model) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(pollEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) contentWidth() int {
	w := m.termW - marginX*2
	if m.maxWidth > 0 && w > m.maxWidth {
		w = m.maxWidth
	}
	return max(w, 20)
}

// headerH is the height of the breadcrumb/close bar, shown only when there
// is a document to go back to.
func (m Model) headerH() int {
	if len(m.stack) > 0 {
		return 1
	}
	return 0
}

// layout sizes the viewport for the current terminal and chrome.
func (m *Model) layout() {
	h := max(m.termH-1-m.headerH(), 1)
	if !m.ready {
		m.vp = viewport.New(m.termW, h)
		m.ready = true
	} else {
		m.vp.Width, m.vp.Height = m.termW, h
	}
}

// rerender re-renders the source at the current width, preserving scroll.
func (m *Model) rerender() {
	m.rendered = m.renderer.RenderDoc(m.cur.src, m.contentWidth())
	pad := strings.Repeat(" ", marginX)
	m.lines = m.lines[:0]
	m.plain = m.plain[:0]
	for _, l := range m.rendered.Lines {
		m.plain = append(m.plain, ansi.Strip(render.Unscale(l.Text)))
		m.lines = append(m.lines, preclear(l.Text)+pad+l.Text)
	}
	off := m.vp.YOffset
	// The viewport only tracks offsets and height; View draws m.lines itself.
	m.vp.SetContent(strings.Join(m.lines, "\n"))
	m.vp.SetYOffset(off)
	if m.cur.query != "" {
		m.findMatches()
	}
	m.collectLinks()
	m.clearSelection()
	m.hover, m.menu = nil, nil
}

// preclear returns the erase sequence a row must start with. kitty leaves a
// scaled block's lower-row cells marked as occupied after ordinary text
// overwrites its top row, and text drawn into such cells is pushed right
// past them (then wraps, shifting every later row). Erasing the row first
// clears those cells; a scaled heading also clears the row its block will
// extend into. Fillers must not erase: their row belongs to the block above.
func preclear(text string) string {
	switch {
	case render.IsFiller(text):
		return ""
	case render.IsScaled(text):
		return "\x1b[K\x1b[B\x1b[K\x1b[A"
	default:
		return "\x1b[K"
	}
}

func (m *Model) reload() bool {
	if m.cur.path == "" {
		return false
	}
	fi, err := os.Stat(m.cur.path)
	if err != nil || !fi.ModTime().After(m.cur.lastMod) {
		return false
	}
	data, err := os.ReadFile(m.cur.path)
	if err != nil {
		return false
	}
	m.cur.lastMod = fi.ModTime()
	m.cur.src = data
	m.rerender()
	m.notice = "reloaded"
	return true
}

// --- navigation ---------------------------------------------------------

// follow acts on a link destination: in-document anchors scroll, local
// markdown files open on the stack, everything else is handed to Open.
func (m *Model) follow(dest string) {
	if strings.HasPrefix(dest, "#") {
		m.jumpAnchor(dest[1:])
		return
	}
	if strings.Contains(dest, "://") || strings.HasPrefix(dest, "mailto:") {
		if !strings.HasPrefix(dest, "file://") {
			m.openExternal(dest)
			return
		}
		dest = strings.TrimPrefix(dest, "file://")
	}
	path, frag, _ := strings.Cut(dest, "#")
	if p, err := url.PathUnescape(path); err == nil {
		path = p
	}
	if path == "" {
		m.jumpAnchor(frag)
		return
	}
	if !filepath.IsAbs(path) && m.cur.path != "" {
		path = filepath.Join(filepath.Dir(m.cur.path), path)
	}
	if isMarkdown(path) {
		m.push(path, frag)
		return
	}
	m.openExternal(path)
}

func isMarkdown(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}

func (m *Model) openExternal(target string) {
	if m.Open == nil {
		m.notice = "no opener configured"
		return
	}
	if err := m.Open(target); err != nil {
		m.notice = fmt.Sprintf("cannot open %s: %v", target, err)
		return
	}
	m.notice = "opened " + target
}

// push saves the current document and opens path on top of it.
func (m *Model) push(path, frag string) {
	data, err := os.ReadFile(path)
	if err != nil {
		m.notice = fmt.Sprintf("cannot open %s: %v", path, err)
		return
	}
	m.cur.yOffset = m.vp.YOffset
	m.stack = append(m.stack, m.cur)
	m.cur = newDoc(path, data)
	m.layout()
	m.vp.GotoTop()
	m.rerender()
	if frag != "" {
		m.jumpAnchor(frag)
	}
}

// pop returns to the previous document at its old scroll position.
func (m *Model) pop() bool {
	n := len(m.stack)
	if n == 0 {
		return false
	}
	m.cur = m.stack[n-1]
	m.stack = m.stack[:n-1]
	m.layout()
	m.rerender()
	m.vp.SetYOffset(m.cur.yOffset)
	return true
}

func (m *Model) jumpAnchor(frag string) {
	if line, ok := m.rendered.Anchors[frag]; ok {
		m.vp.SetYOffset(line)
		return
	}
	if line, ok := m.rendered.Anchors[render.Slug(frag)]; ok {
		m.vp.SetYOffset(line)
		return
	}
	m.notice = "no heading #" + frag
}

// --- search -------------------------------------------------------------

func (m *Model) findMatches() {
	m.cur.matches = m.cur.matches[:0]
	q := strings.ToLower(m.cur.query)
	for i, l := range m.plain {
		if strings.Contains(strings.ToLower(l), q) {
			m.cur.matches = append(m.cur.matches, i)
		}
	}
}

func (m *Model) jumpToMatch(delta int) {
	n := len(m.cur.matches)
	if n == 0 {
		return
	}
	m.cur.matchIdx = ((m.cur.matchIdx+delta)%n + n) % n
	m.vp.SetYOffset(m.cur.matches[m.cur.matchIdx])
}

func (m *Model) startSearchFrom() {
	// Pick the first match at or below the current top line.
	m.cur.matchIdx = 0
	for i, ln := range m.cur.matches {
		if ln >= m.vp.YOffset {
			m.cur.matchIdx = i
			break
		}
	}
	if len(m.cur.matches) > 0 {
		m.vp.SetYOffset(m.cur.matches[m.cur.matchIdx])
	}
}

// --- bubbletea ----------------------------------------------------------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termW, m.termH = msg.Width, msg.Height
		m.layout()
		m.rerender()
		return m, nil

	case tickMsg:
		m.reload()
		return m, tick()

	case tea.MouseMsg:
		m.osc52 = ""
		return m.handleMouse(msg)

	case tea.KeyMsg:
		m.notice = ""
		m.osc52 = ""
		if m.menu != nil {
			mm, cmd, handled := m.handleMenuKey(msg)
			if handled {
				return mm, cmd
			}
		}
		if m.searching {
			switch {
			case key.Matches(msg, keys.Cancel):
				m.searching = false
				m.search.Blur()
				return m, nil
			case key.Matches(msg, keys.Accept):
				m.searching = false
				m.search.Blur()
				m.cur.query = strings.TrimSpace(m.search.Value())
				if m.cur.query == "" {
					m.cur.matches = nil
					return m, nil
				}
				m.findMatches()
				m.startSearchFrom()
				if len(m.cur.matches) == 0 {
					m.notice = "no matches"
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			return m, cmd
		}

		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Back):
			// Esc peels one layer at a time: focus/selection, then the doc.
			if m.focus >= 0 || m.sel.active || m.hover != nil {
				m.focus, m.hover = -1, nil
				m.clearSelection()
				return m, nil
			}
			if !m.pop() {
				return m, tea.Quit
			}
			return m, nil
		case key.Matches(msg, keys.NextLink):
			m.focusLink(1)
			return m, nil
		case key.Matches(msg, keys.PrevLink):
			m.focusLink(-1)
			return m, nil
		case key.Matches(msg, keys.Follow):
			if m.focus >= 0 && m.focus < len(m.links) {
				dest := m.links[m.focus].link.Dest
				m.focus = -1
				m.follow(dest)
			}
			return m, nil
		case key.Matches(msg, keys.Copy):
			m.copyText(m.selectedText())
			return m, nil
		case key.Matches(msg, keys.Top):
			m.vp.GotoTop()
			return m, nil
		case key.Matches(msg, keys.Bottom):
			m.vp.GotoBottom()
			return m, nil
		case key.Matches(msg, keys.Search):
			m.searching = true
			m.search.SetValue("")
			return m, m.search.Focus()
		case key.Matches(msg, keys.Next):
			m.jumpToMatch(1)
			return m, nil
		case key.Matches(msg, keys.Prev):
			m.jumpToMatch(-1)
			return m, nil
		case key.Matches(msg, keys.Reload):
			m.cur.lastMod = time.Time{}
			if !m.reload() {
				m.notice = "nothing to reload"
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if !m.ready {
		return ""
	}
	// Draw visible lines without lipgloss padding: a kitty-scaled heading is
	// wider than its measured width, and padding would wrap onto the row
	// below and corrupt the block.
	top := max(0, m.vp.YOffset)
	bottom := min(top+m.vp.Height, len(m.lines))
	visible := make([]string, m.vp.Height)
	copy(visible, m.lines[top:bottom])
	n := bottom - top
	if n > 0 && n == m.vp.Height {
		// A scaled block on the last row would extend past the screen.
		visible[n-1] = render.Unscale(visible[n-1])
	}
	for i := 0; i < n; i++ {
		// A filler only skips cells; if its heading is not the row above in
		// this frame (scrolled off, or a stale block from the previous
		// frame), draw an empty line instead so the row is actually erased.
		if render.IsFiller(visible[i]) && (i == 0 || !render.IsScaled(visible[i-1])) {
			visible[i] = ""
		}
	}

	var rows []string
	if m.headerH() > 0 {
		rows = append(rows, m.header())
	}
	rows = append(rows, visible...)
	m.drawSelection(rows)
	m.drawFocus(rows)
	rows = append(rows, m.statusBar())

	if ref := m.tooltipTarget(); ref != nil {
		if box, x, y, ok := m.tooltip(*ref); ok {
			flatten(rows, y)
			rows[y] = splice(rows[y], x, box)
		}
	}
	if m.menu != nil {
		box, x, y := m.menuBox()
		for i, r := range box {
			if y+i < len(rows) {
				flatten(rows, y+i)
				rows[y+i] = splice(rows[y+i], x, r)
			}
		}
	}
	if m.osc52 != "" {
		rows[len(rows)-1] += m.osc52
	}
	return strings.Join(rows, "\n")
}

// tooltipTarget is the link whose destination to show: the focused one,
// else the hovered one.
func (m Model) tooltipTarget() *linkRef {
	if m.focus >= 0 && m.focus < len(m.links) {
		ref := m.links[m.focus]
		return &ref
	}
	return m.hover
}

// drawSelection paints the selected cell range onto the visible rows.
func (m Model) drawSelection(rows []string) {
	if !m.sel.active {
		return
	}
	th := m.renderer.Theme()
	sty := lipgloss.NewStyle().Background(th.Accent).Foreground(th.Mantle)
	a, b := m.sel.ordered()
	for line := a.line; line <= b.line; line++ {
		row, ok := m.screenRow(line)
		if !ok || line >= len(m.plain) {
			continue
		}
		flatten(rows, row)
		s := m.lineScale(line)
		from, to := 0, ansi.StringWidth(m.plain[line])
		if line == a.line {
			from = a.col / s
		}
		if line == b.line {
			to = min(b.col/s+1, max(to, b.col/s+1))
		}
		rows[row] = highlight(rows[row], marginX+from, marginX+to, sty)
	}
}

// drawFocus marks the keyboard-focused link.
func (m Model) drawFocus(rows []string) {
	if m.focus < 0 || m.focus >= len(m.links) {
		return
	}
	ref := m.links[m.focus]
	row, ok := m.screenRow(ref.line)
	if !ok {
		return
	}
	th := m.renderer.Theme()
	sty := lipgloss.NewStyle().Background(th.Link).Foreground(th.Mantle).Bold(true)
	s := m.lineScale(ref.line)
	flatten(rows, row)
	rows[row] = highlight(rows[row], marginX+ref.link.Start/s, marginX+ref.link.End/s, sty)
}

// header draws the breadcrumb trail and the ✕ close button.
func (m Model) header() string {
	th := m.renderer.Theme()
	bar := lipgloss.NewStyle().Background(th.Mantle).Foreground(th.Muted)
	cur := bar.Foreground(th.Text).Bold(true)
	closeBtn := lipgloss.NewStyle().Background(th.Quote).Foreground(th.Mantle).Bold(true).Render(" ✕ ")

	var crumbs []string
	for _, d := range m.stack {
		crumbs = append(crumbs, bar.Render(d.name()))
	}
	crumbs = append(crumbs, cur.Render(m.cur.name()))
	left := bar.Render(" ") + strings.Join(crumbs, bar.Render(" › "))

	gap := m.termW - lipgloss.Width(left) - closeW
	if gap < 0 {
		left = ansi.Truncate(left, m.termW-closeW-1, "…")
		gap = max(m.termW-lipgloss.Width(left)-closeW, 0)
	}
	return left + bar.Render(strings.Repeat(" ", gap)) + closeBtn
}

func (m Model) statusBar() string {
	th := m.renderer.Theme()
	bar := lipgloss.NewStyle().Background(th.Mantle).Foreground(th.Subtle)
	name := lipgloss.NewStyle().Background(th.Accent).Foreground(th.Mantle).Bold(true).Padding(0, 1)
	dim := bar.Foreground(th.Muted)

	if m.searching {
		left := name.Render("search")
		field := bar.Render(" " + m.search.View())
		return padBar(left+field, m.termW, bar)
	}

	title := m.cur.path
	if title == "" {
		title = "stdin"
	}
	left := name.Render(title)
	if m.notice != "" {
		left += bar.Render(" " + m.notice)
	} else if m.cur.query != "" {
		if len(m.cur.matches) == 0 {
			left += bar.Render(fmt.Sprintf(" /%s: none", m.cur.query))
		} else {
			left += bar.Render(fmt.Sprintf(" /%s: %d/%d", m.cur.query, m.cur.matchIdx+1, len(m.cur.matches)))
		}
	}

	hint := "j/k scroll · / search · tab links · y copy · right-click menu · q quit"
	if len(m.stack) > 0 {
		hint = "esc back · " + hint
	}
	pct := fmt.Sprintf(" %3.0f%% ", m.vp.ScrollPercent()*100)
	right := dim.Render(hint) + bar.Bold(true).Render(pct)

	gap := m.termW - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		right = bar.Bold(true).Render(pct)
		gap = max(m.termW-lipgloss.Width(left)-lipgloss.Width(right), 0)
	}
	return left + bar.Render(strings.Repeat(" ", gap)) + right
}

func padBar(s string, width int, style lipgloss.Style) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + style.Render(strings.Repeat(" ", width-w))
}
