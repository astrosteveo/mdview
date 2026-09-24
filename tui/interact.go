package tui

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/astrosteveo/mdview/render"
)

// Mouse and keyboard interaction beyond scrolling: link hover/focus with
// tooltips, text selection, clipboard, and the context menu.

const doubleClick = 400 * time.Millisecond

type pos struct{ line, col int } // document line, content cell column

type linkRef struct {
	line int
	link render.Link
}

type selection struct {
	active   bool // a range exists and is drawn
	dragging bool // left button is down since the anchor was set
	a, b     pos
}

func (s selection) ordered() (pos, pos) {
	if s.b.line < s.a.line || (s.b.line == s.a.line && s.b.col < s.a.col) {
		return s.b, s.a
	}
	return s.a, s.b
}

type menuItem struct {
	label string
	run   func(m *Model) tea.Cmd
}

type menu struct {
	x, y  int
	items []menuItem
	idx   int
}

var errNoClipboardTool = errors.New("no clipboard tool found")

// defaultCopy pipes text to the first clipboard tool found. Callers fall
// back to OSC 52 (the terminal's own clipboard) when none is installed.
func defaultCopy(text string) error {
	for _, c := range [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
		{"pbcopy"},
	} {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return errNoClipboardTool
}

// --- geometry -----------------------------------------------------------

// hit maps a terminal cell to a document position. ok is false outside the
// content area; the position is clamped to the document for drags.
func (m Model) hit(x, y int) (p pos, ok bool) {
	row := y - m.headerH()
	if row < 0 || row >= m.vp.Height || len(m.lines) == 0 {
		return pos{}, false
	}
	line := min(m.vp.YOffset+row, len(m.lines)-1)
	// The row under a scaled heading is the lower half of its block.
	if line > 0 && render.IsFiller(m.rendered.Lines[line].Text) && render.IsScaled(m.rendered.Lines[line-1].Text) {
		line--
	}
	return pos{line, max(x-marginX, 0)}, true
}

// lineScale is the cell width of one character on a document line: 2 on a
// kitty-scaled heading, else 1.
func (m Model) lineScale(line int) int {
	if line >= 0 && line < len(m.rendered.Lines) && render.IsScaled(m.rendered.Lines[line].Text) {
		return 2
	}
	return 1
}

func (m Model) screenRow(line int) (int, bool) {
	row := line - m.vp.YOffset
	if row < 0 || row >= m.vp.Height {
		return 0, false
	}
	return row + m.headerH(), true
}

func (m Model) linkAt(p pos) (linkRef, bool) {
	l, ok := m.rendered.LinkAt(p.line, p.col)
	return linkRef{p.line, l}, ok
}

// collectLinks flattens the document's links in reading order.
func (m *Model) collectLinks() {
	m.links = m.links[:0]
	for i, l := range m.rendered.Lines {
		for _, lk := range l.Links {
			m.links = append(m.links, linkRef{i, lk})
		}
	}
	m.focus = -1
}

// --- link focus (keyboard) ---------------------------------------------

func (m *Model) focusLink(delta int) {
	n := len(m.links)
	if n == 0 {
		m.notice = "no links"
		return
	}
	if m.focus < 0 {
		// Start from the first link on screen (or the last one above it).
		m.focus = 0
		for i, l := range m.links {
			if l.line >= m.vp.YOffset {
				m.focus = i
				break
			}
		}
		if delta < 0 {
			m.focus = (m.focus - 1 + n) % n
		}
	} else {
		m.focus = ((m.focus+delta)%n + n) % n
	}
	m.hover = nil
	m.scrollTo(m.links[m.focus].line)
}

// scrollTo makes a document line visible without moving if it already is.
func (m *Model) scrollTo(line int) {
	if line < m.vp.YOffset || line >= m.vp.YOffset+m.vp.Height {
		m.vp.SetYOffset(max(line-m.vp.Height/2, 0))
	}
}

// --- selection ----------------------------------------------------------

func (m *Model) clearSelection() { m.sel = selection{} }

// selectWord selects the whitespace-delimited word around p.
func (m *Model) selectWord(p pos) {
	if p.line >= len(m.plain) {
		return
	}
	s := m.lineScale(p.line)
	text := m.plain[p.line]
	cells := []rune(text)
	c := p.col / s
	if c >= len(cells) || unicode.IsSpace(cells[c]) {
		return
	}
	a, b := c, c
	for a > 0 && !unicode.IsSpace(cells[a-1]) {
		a--
	}
	for b+1 < len(cells) && !unicode.IsSpace(cells[b+1]) {
		b++
	}
	// Convert rune indexes back to cells.
	m.sel = selection{active: true, a: pos{p.line, ansi.StringWidth(string(cells[:a])) * s},
		b: pos{p.line, (ansi.StringWidth(string(cells[:b+1])) - 1) * s}}
}

// selectedText returns the selection as plain text, one line per row.
func (m Model) selectedText() string {
	if !m.sel.active {
		return ""
	}
	a, b := m.sel.ordered()
	var out []string
	copiedBlocks := map[int]bool{}
	for i := a.line; i <= b.line && i < len(m.plain); i++ {
		if i < len(m.rendered.Lines) {
			l := m.rendered.Lines[i]
			if l.Mermaid != nil {
				if _, ok := m.diagrams[l.Mermaid.ID]; ok {
					if !copiedBlocks[l.Mermaid.ID] {
						out = append(out, l.Mermaid.Source)
						copiedBlocks[l.Mermaid.ID] = true
					}
					continue
				}
			}
		}
		s := m.lineScale(i)
		text := m.plain[i]
		from, to := 0, ansi.StringWidth(text)
		if i == a.line {
			from = a.col / s
		}
		if i == b.line {
			to = min(b.col/s+1, to)
		}
		if from >= to {
			out = append(out, "")
			continue
		}
		out = append(out, strings.TrimRight(ansi.Cut(text, from, to), " "))
	}
	return strings.Join(out, "\n")
}

func (m *Model) copyText(text string) {
	if text == "" {
		m.notice = "nothing to copy"
		return
	}
	copyFn := m.Copy
	if copyFn == nil {
		copyFn = defaultCopy
	}
	err := copyFn(text)
	if errors.Is(err, errNoClipboardTool) {
		m.osc52 = "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x1b\\"
		err = nil
	}
	if err != nil {
		m.notice = "copy failed: " + err.Error()
		return
	}
	m.notice = fmt.Sprintf("copied %d chars", len([]rune(text)))
}

// --- context menu -------------------------------------------------------

func (m *Model) openMenu(x, y int) {
	var items []menuItem
	if p, ok := m.hit(x, y); ok {
		if d, ok := m.diagramAt(p); ok {
			items = append(items, menuItem{"Open diagram", func(m *Model) tea.Cmd { m.openDiagram(d.block.ID); return nil }}, menuItem{"Copy Mermaid source", func(m *Model) tea.Cmd { m.copyText(d.block.Source); return nil }})
		} else if ref, ok := m.linkAt(p); ok {
			dest := ref.link.Dest
			items = append(items,
				menuItem{"Open link", func(m *Model) tea.Cmd { m.follow(dest); return nil }},
				menuItem{"Copy link address", func(m *Model) tea.Cmd { m.copyText(dest); return nil }},
			)
		}
	}
	if m.sel.active {
		items = append(items, menuItem{"Copy selection", func(m *Model) tea.Cmd { m.copyText(m.selectedText()); return nil }})
	}
	if len(m.stack) > 0 {
		items = append(items, menuItem{"Back", func(m *Model) tea.Cmd { m.pop(); return nil }})
	}
	if m.cur.path != "" {
		items = append(items, menuItem{"Reload", func(m *Model) tea.Cmd {
			m.cur.lastMod = time.Time{}
			if !m.reload() {
				m.notice = "nothing to reload"
			}
			return nil
		}})
	}
	items = append(items, menuItem{"Quit", func(m *Model) tea.Cmd { return tea.Quit }})
	m.menu = &menu{x: x, y: y, items: items}
	m.hover = nil
}

func (m *Model) closeMenu() { m.menu = nil }

// menuWindow keeps the selected entry visible, even in a one-row terminal.
func (m Model) menuWindow() (start, count, border int) {
	if m.termH >= 3 && m.termW >= 4 {
		border = 1
	}
	count = min(len(m.menu.items), max(m.termH-2*border, 0))
	start = max(0, m.menu.idx-count+1)
	start = min(start, len(m.menu.items)-count)
	return
}

// menuBox renders only the visible entries and clamps the entire box.
func (m Model) menuBox() (rows []string, x, y int) {
	th := m.renderer.Theme()
	start, count, edge := m.menuWindow()
	w := 0
	for _, it := range m.menu.items {
		w = max(w, ansi.StringWidth(it.label))
	}
	padding := 2 * edge
	w = min(w, max(m.termW-2*edge-padding, 0))
	item := lipgloss.NewStyle().Background(th.Surface0).Foreground(th.Text)
	cur := item.Background(th.Accent).Foreground(th.Mantle).Bold(true)
	border := item.Foreground(th.Muted)
	if edge > 0 {
		rows = append(rows, border.Render("╭"+strings.Repeat("─", w+padding)+"╮"))
	}
	for i := start; i < start+count; i++ {
		sty := item
		if i == m.menu.idx {
			sty = cur
		}
		label := padTo(ansi.Truncate(m.menu.items[i].label, w, "…"), w)
		if edge > 0 {
			label = " " + label + " "
		}
		row := sty.Render(label)
		if edge > 0 {
			row = border.Render("│") + row + border.Render("│")
		}
		rows = append(rows, row)
	}
	if edge > 0 {
		rows = append(rows, border.Render("╰"+strings.Repeat("─", w+padding)+"╯"))
	}
	x = max(0, min(m.menu.x, m.termW-(w+padding+2*edge)))
	y = max(0, min(m.menu.y, m.termH-len(rows)))
	return
}

func (m Model) menuHit(x, y int) int {
	rows, mx, my := m.menuBox()
	if len(rows) == 0 {
		return -1
	}
	start, count, edge := m.menuWindow()
	i := y - my - edge
	if x < mx+edge || x >= mx+ansi.StringWidth(rows[0])-edge || i < 0 || i >= count {
		return -1
	}
	return start + i
}

func (m *Model) runMenuItem(i int) tea.Cmd {
	items := m.menu.items
	m.closeMenu()
	if i < 0 || i >= len(items) {
		return nil
	}
	return items[i].run(m)
}

// --- tooltip ------------------------------------------------------------

// tooltip renders the destination chip for a link and where to draw it:
// the row below the link, or above when the link is on the last row.
func (m Model) tooltip(ref linkRef) (box string, x, y int, ok bool) {
	row, visible := m.screenRow(ref.line)
	if !visible {
		return "", 0, 0, false
	}
	th := m.renderer.Theme()
	icon := "↗"
	if strings.HasPrefix(ref.link.Dest, "#") {
		icon = "§"
	} else if !strings.Contains(ref.link.Dest, "://") && isMarkdown(strings.SplitN(ref.link.Dest, "#", 2)[0]) {
		icon = "⏎"
	}
	text := " " + icon + " " + ref.link.Dest + " "
	x = marginX + ref.link.Start
	maxW := max(m.termW-x, 8)
	if ansi.StringWidth(text) > maxW {
		x = max(m.termW-ansi.StringWidth(text), 0)
		maxW = m.termW - x
		text = ansi.Truncate(text, maxW, "…")
	}
	box = lipgloss.NewStyle().Background(th.Surface1).Foreground(th.Text).Render(text)
	y = row + 1
	if y >= m.headerH()+m.vp.Height {
		y = row - 1
	}
	if y < m.headerH() {
		return "", 0, 0, false
	}
	return box, x, y, true
}

// --- event handling -----------------------------------------------------

func (m Model) handleMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	if m.menu != nil {
		switch {
		case msg.Button == tea.MouseButtonWheelDown:
			m.menu.idx = min(m.menu.idx+1, len(m.menu.items)-1)
			return m, nil
		case msg.Button == tea.MouseButtonWheelUp:
			m.menu.idx = max(m.menu.idx-1, 0)
			return m, nil
		case msg.Action == tea.MouseActionMotion:
			if i := m.menuHit(msg.X, msg.Y); i >= 0 {
				m.menu.idx = i
			}
			return m, nil
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
			i := m.menuHit(msg.X, msg.Y)
			return m, m.runMenuItem(i)
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonRight:
			m.openMenu(msg.X, msg.Y)
			return m, nil
		case msg.Action == tea.MouseActionPress:
			m.closeMenu()
			return m, nil
		}
		return m, nil
	}

	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonRight:
		m.openMenu(msg.X, msg.Y)
		return m, nil

	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		m.notice = ""
		m.focus = -1
		if m.headerH() > 0 && msg.Y == 0 {
			m.clearSelection()
			m.clickBreadcrumb(msg.X)
			return m, nil
		}
		p, ok := m.hit(msg.X, msg.Y)
		if !ok {
			m.clearSelection()
			return m, nil
		}
		now := time.Now()
		if now.Sub(m.lastClick) < doubleClick && m.lastClickAt == p {
			// Double click: select the word under the pointer.
			m.selectWord(p)
			m.lastClick = time.Time{}
			return m, nil
		}
		m.lastClick, m.lastClickAt = now, p
		m.sel = selection{dragging: true, a: p, b: p}
		return m, nil

	case msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft && m.sel.dragging:
		if p, ok := m.hit(msg.X, msg.Y); ok && p != m.sel.a {
			m.sel.b, m.sel.active = p, true
			m.hover = nil
		}
		return m, nil

	case msg.Action == tea.MouseActionRelease && m.sel.dragging:
		m.sel.dragging = false
		if m.sel.active {
			return m, nil
		}
		// A plain click: follow a link, otherwise just drop any selection.
		anchor := m.sel.a
		m.clearSelection()
		if d, ok := m.diagramAt(anchor); ok {
			m.openDiagram(d.block.ID)
		} else if ref, ok := m.linkAt(anchor); ok {
			m.follow(ref.link.Dest)
		}
		return m, nil

	case msg.Action == tea.MouseActionMotion:
		m.hover = nil
		if p, ok := m.hit(msg.X, msg.Y); ok {
			if ref, ok := m.linkAt(p); ok {
				m.hover = &ref
				m.focus = -1
			}
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// handleMenuKey drives an open menu; handled is false for keys it ignores.
func (m Model) handleMenuKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc", "q":
		m.closeMenu()
	case "up", "k", "shift+tab":
		m.menu.idx = (m.menu.idx - 1 + len(m.menu.items)) % len(m.menu.items)
	case "down", "j", "tab":
		m.menu.idx = (m.menu.idx + 1) % len(m.menu.items)
	case "enter", " ":
		return m, m.runMenuItem(m.menu.idx), true
	}
	return m, nil, true
}
