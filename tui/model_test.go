package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"mdview/render"
)

func setup(t *testing.T) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	root := filepath.Join(dir, "root.md")
	os.WriteFile(root, []byte("# Root\n\nGo to [child](sub/child.md#part-two) or [web](https://example.com).\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "sub", "child.md"),
		[]byte("# Child\n\n"+strings.Repeat("filler\n\n", 30)+"## Part Two\n\nback via [root](../root.md)\n"), 0o644)

	src, _ := os.ReadFile(root)
	r := render.New(render.Mocha())
	r.NoURLs = true // as main does for the pager: destinations become tooltips
	m := New(root, src, r, 0)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return mm.(Model), dir
}

func click(m Model, x, y int) Model {
	mm, _ := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	mm, _ = mm.(Model).Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	return mm.(Model)
}

func mouse(m Model, x, y int, action tea.MouseAction, button tea.MouseButton) Model {
	mm, _ := m.Update(tea.MouseMsg{X: x, Y: y, Action: action, Button: button})
	return mm.(Model)
}

func press(m Model, k string) (Model, tea.Cmd) {
	var msg tea.KeyMsg
	switch k {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	mm, cmd := m.Update(msg)
	return mm.(Model), cmd
}

// colOf finds the terminal column of text on the first visible line containing it.
func colOf(t *testing.T, m Model, text string) (x, y int) {
	t.Helper()
	for i := m.vp.YOffset; i < len(m.lines) && i < m.vp.YOffset+m.vp.Height; i++ {
		plain := ansi.Strip(m.lines[i])
		if c := strings.Index(plain, text); c >= 0 {
			return c, i - m.vp.YOffset + m.headerH()
		}
	}
	t.Fatalf("%q not visible", text)
	return 0, 0
}

func TestClickFollowsMarkdownLinkAndBack(t *testing.T) {
	m, dir := setup(t)

	x, y := colOf(t, m, "child")
	m = click(m, x, y)
	if want := filepath.Join(dir, "sub", "child.md"); m.cur.path != want {
		t.Fatalf("after click cur.path = %q, want %q (notice %q)", m.cur.path, want, m.notice)
	}
	if len(m.stack) != 1 || m.headerH() != 1 {
		t.Fatalf("stack=%d header=%d", len(m.stack), m.headerH())
	}
	// The anchor is near the end, so the offset clamps; it must be on screen.
	if line := m.rendered.Anchors["part-two"]; line == 0 || line < m.vp.YOffset || line >= m.vp.YOffset+m.vp.Height || m.vp.YOffset == 0 {
		t.Fatalf("anchor jump: YOffset=%d height=%d anchor line %d", m.vp.YOffset, m.vp.Height, line)
	}
	if !strings.Contains(ansi.Strip(m.View()), "root.md › child.md") || !strings.Contains(m.View(), "✕") {
		t.Fatalf("header missing breadcrumb/close:\n%s", ansi.Strip(m.View()))
	}

	// Esc returns to the root at its old offset.
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(Model)
	if cmd != nil || len(m.stack) != 0 || filepath.Base(m.cur.path) != "root.md" || m.vp.YOffset != 0 {
		t.Fatalf("esc did not pop cleanly: stack=%d path=%q off=%d", len(m.stack), m.cur.path, m.vp.YOffset)
	}

	// Esc at the root quits.
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc}); cmd == nil {
		t.Fatal("esc at root should quit")
	}
}

func TestCloseButtonAndRelativeLinkFromChild(t *testing.T) {
	m, _ := setup(t)
	x, y := colOf(t, m, "child")
	m = click(m, x, y)
	m.vp.GotoBottom()

	// Follow ../root.md from inside sub/: resolves against the child's dir.
	x, y = colOf(t, m, "root")
	m = click(m, x, y)
	if len(m.stack) != 2 || filepath.Base(m.cur.path) != "root.md" {
		t.Fatalf("relative link from child failed: stack=%d path=%q notice=%q", len(m.stack), m.cur.path, m.notice)
	}

	// ✕ sits in the last closeW cells of row 0.
	m = click(m, m.termW-1, 0)
	if len(m.stack) != 1 || filepath.Base(m.cur.path) != "child.md" {
		t.Fatalf("close button did not pop: stack=%d path=%q", len(m.stack), m.cur.path)
	}
	// Clicking elsewhere on the header does nothing.
	m = click(m, 2, 0)
	if len(m.stack) != 1 {
		t.Fatal("header click outside ✕ should be inert")
	}
}

func TestExternalLinkUsesOpener(t *testing.T) {
	m, _ := setup(t)
	var opened string
	m.Open = func(target string) error { opened = target; return nil }
	x, y := colOf(t, m, "web")
	m = click(m, x, y)
	if opened != "https://example.com" || len(m.stack) != 0 {
		t.Fatalf("opened=%q stack=%d", opened, len(m.stack))
	}
	if !strings.Contains(m.notice, "opened") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestClickOnPlainTextIsInert(t *testing.T) {
	m, _ := setup(t)
	x, y := colOf(t, m, "Go to")
	m = click(m, x, y)
	if len(m.stack) != 0 || m.notice != "" {
		t.Fatalf("plain text click changed state: stack=%d notice=%q", len(m.stack), m.notice)
	}
}

func TestOrphanedFillerIsErased(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "h.md")
	src := []byte("# Big heading\n\n" + strings.Repeat("para\n\n", 20))
	os.WriteFile(path, src, 0o644)
	r := render.New(render.Mocha())
	r.BigHeadings = true
	m := New(path, src, r, 0)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	m = mm.(Model)

	rows := strings.Split(m.View(), "\n")
	if !render.IsScaled(rows[0]) || !render.IsFiller(rows[1]) {
		t.Fatalf("expected heading + filler at top, got %q / %q", rows[0], rows[1])
	}
	if !strings.HasPrefix(rows[0], "\x1b[K\x1b[B\x1b[K\x1b[A") || !strings.HasPrefix(rows[2], "\x1b[K") {
		t.Fatalf("rows must pre-erase themselves: %q / %q", rows[0], rows[2])
	}

	// Scroll one line: the filler is now the first row with no heading above
	// it, so it must become an empty line rather than a cell-skipping spacer.
	m.vp.SetYOffset(1)
	rows = strings.Split(m.View(), "\n")
	if rows[0] != "" {
		t.Fatalf("orphaned filler not erased: %q", rows[0])
	}
}

func TestHoverShowsTooltipAndInlineURLIsGone(t *testing.T) {
	m, _ := setup(t)
	if strings.Contains(ansi.Strip(m.View()), "(sub/child.md#part-two)") {
		t.Fatal("pager should not render destinations inline")
	}
	x, y := colOf(t, m, "child")
	m = mouse(m, x, y, tea.MouseActionMotion, tea.MouseButtonNone)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "⏎ sub/child.md#part-two") {
		t.Fatalf("tooltip missing on hover:\n%s", view)
	}
	// Leaving the link hides it.
	m = mouse(m, 0, y+3, tea.MouseActionMotion, tea.MouseButtonNone)
	if strings.Contains(ansi.Strip(m.View()), "sub/child.md#part-two") {
		t.Fatal("tooltip should hide when the pointer leaves the link")
	}
}

func TestKeyboardLinkFocus(t *testing.T) {
	m, dir := setup(t)
	m, _ = press(m, "tab")
	if m.focus != 0 || !strings.Contains(ansi.Strip(m.View()), "⏎ sub/child.md#part-two") {
		t.Fatalf("tab should focus the first link and show its tooltip (focus=%d)", m.focus)
	}
	m, _ = press(m, "tab")
	if m.focus != 1 || !strings.Contains(ansi.Strip(m.View()), "↗ https://example.com") {
		t.Fatalf("second tab should focus the web link (focus=%d)", m.focus)
	}
	m, _ = press(m, "shift+tab")
	if m.focus != 0 {
		t.Fatalf("shift+tab should go back (focus=%d)", m.focus)
	}
	m, _ = press(m, "esc")
	if m.focus != -1 || len(m.stack) != 0 {
		t.Fatal("esc should clear focus before popping")
	}
	m, _ = press(m, "tab")
	m, _ = press(m, "enter")
	if filepath.Base(m.cur.path) != "child.md" || len(m.stack) != 1 {
		t.Fatalf("enter should follow the focused link: %q", m.cur.path)
	}
	_ = dir
}

func TestDragSelectsAndCopies(t *testing.T) {
	m, _ := setup(t)
	var copied string
	m.Copy = func(s string) error { copied = s; return nil }
	x, y := colOf(t, m, "Go to")
	m = mouse(m, x, y, tea.MouseActionPress, tea.MouseButtonLeft)
	m = mouse(m, x+4, y, tea.MouseActionMotion, tea.MouseButtonLeft)
	m = mouse(m, x+4, y, tea.MouseActionRelease, tea.MouseButtonLeft)
	if !m.sel.active || m.selectedText() != "Go to" {
		t.Fatalf("selection = %q active=%v", m.selectedText(), m.sel.active)
	}
	m, _ = press(m, "y")
	if copied != "Go to" || !strings.Contains(m.notice, "copied") {
		t.Fatalf("copied=%q notice=%q", copied, m.notice)
	}
	// Dragging over a link must not follow it.
	if len(m.stack) != 0 {
		t.Fatal("drag should not navigate")
	}
	// Selection spanning lines joins with newlines (drag upwards: the
	// anchor becomes the end). Rows above: heading, rule, blank.
	m.lastClick = time.Time{} // not a double click
	m = mouse(m, x, y, tea.MouseActionPress, tea.MouseButtonLeft)
	m = mouse(m, x+1, y-3, tea.MouseActionMotion, tea.MouseButtonLeft)
	m = mouse(m, x+1, y-3, tea.MouseActionRelease, tea.MouseButtonLeft)
	got := m.selectedText()
	if strings.Count(got, "\n") != 3 || !strings.HasPrefix(got, "oot") || !strings.HasSuffix(got, "\nG") {
		t.Fatalf("multi-line selection = %q", got)
	}
	// Esc clears the selection first, then (second press) pops/quits.
	m, cmd := press(m, "esc")
	if m.sel.active || cmd != nil {
		t.Fatal("first esc should only clear the selection")
	}
}

func TestDoubleClickSelectsWord(t *testing.T) {
	m, _ := setup(t)
	x, y := colOf(t, m, "Go to")
	m = click(m, x+1, y) // single click: anchor only
	m = click(m, x+1, y) // second click within the double-click window
	if got := m.selectedText(); got != "Go" {
		t.Fatalf("double click selected %q", got)
	}
}

func TestContextMenu(t *testing.T) {
	m, _ := setup(t)
	var copied string
	m.Copy = func(s string) error { copied = s; return nil }
	x, y := colOf(t, m, "web")
	m = mouse(m, x, y, tea.MouseActionPress, tea.MouseButtonRight)
	if m.menu == nil {
		t.Fatal("right click should open the menu")
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Open link", "Copy link address", "Reload", "Quit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("menu missing %q:\n%s", want, view)
		}
	}
	m, _ = press(m, "j") // → Copy link address
	m, _ = press(m, "enter")
	if m.menu != nil || copied != "https://example.com" {
		t.Fatalf("menu action failed: menu=%v copied=%q", m.menu != nil, copied)
	}

	// Mouse: open, hover the last item (Quit), click it.
	m = mouse(m, x, y, tea.MouseActionPress, tea.MouseButtonRight)
	rows, mx, my := m.menuBox()
	last := len(m.menu.items) - 1
	m = mouse(m, mx+2, my+1+last, tea.MouseActionMotion, tea.MouseButtonNone)
	if m.menu.idx != last {
		t.Fatalf("hover should select item %d, got %d (%d rows)", last, m.menu.idx, len(rows))
	}
	quit, cmd := m.Update(tea.MouseMsg{X: mx + 2, Y: my + 1 + last, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd == nil {
		t.Fatal("clicking Quit should return tea.Quit")
	}
	// Esc closes without acting.
	mm := quit.(Model)
	mm = mouse(mm, x, y, tea.MouseActionPress, tea.MouseButtonRight)
	mm, _ = press(mm, "esc")
	if mm.menu != nil || len(mm.stack) != 0 {
		t.Fatal("esc should close the menu only")
	}
}
