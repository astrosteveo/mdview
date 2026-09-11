package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	m := New(root, src, render.New(render.Mocha()), 0)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return mm.(Model), dir
}

func click(m Model, x, y int) Model {
	mm, _ := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	return mm.(Model)
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
