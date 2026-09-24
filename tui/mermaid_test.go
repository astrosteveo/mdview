package tui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astrosteveo/mdview/graphics"
	"github.com/astrosteveo/mdview/mermaid"
	"github.com/astrosteveo/mdview/render"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const diagramSource = "flowchart LR\n A[Needle 世界] --> B"
const diagramMarkdown = "```mermaid\n" + diagramSource + "\n```\n"

func testImage() *mermaid.Image {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1600, 1600)))
	return &mermaid.Image{PNG: b.Bytes(), Width: 1600, Height: 1600}
}
func diagramModel(t *testing.T, src string, fn mermaid.RenderFunc) Model {
	t.Helper()
	m := New("", []byte(src), render.New(render.Mocha()), 0)
	session := mermaid.New(fn, "test", "mocha")
	writer := graphics.NewWriter(io.Discard)
	t.Cleanup(func() { session.Close(); writer.Close() })
	m.EnableMermaid(session, writer, graphics.Capability{CellWidth: 10, CellHeight: 20})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 15})
	return mm.(Model)
}
func readyDiagrams(t *testing.T, m Model, n int) Model {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mm, _ := m.Update(tickMsg(time.Now()))
		m = mm.(Model)
		if len(m.diagrams) == n {
			return m
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("got %d diagrams, want %d", len(m.diagrams), n)
	return m
}
func successRender(context.Context, string, string) (*mermaid.Image, error) { return testImage(), nil }

func TestAsyncDiagramsPreserveScrollAndSilentSource(t *testing.T) {
	gate := make(chan struct{})
	m := diagramModel(t, diagramMarkdown+"\n# After\n\n"+strings.Repeat("tail\n\n", 30), func(ctx context.Context, _, _ string) (*mermaid.Image, error) {
		select {
		case <-gate:
			return testImage(), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if !strings.Contains(m.rendered.Text(), "Needle") || len(m.diagrams) != 0 {
		t.Fatal("source not visible during render")
	}
	m.jumpAnchor("after")
	before := m.plain[m.vp.YOffset]
	if !strings.Contains(before, "After") {
		t.Fatal("test anchor not at top")
	}
	close(gate)
	m = readyDiagrams(t, m, 1)
	if m.plain[m.vp.YOffset] != before {
		t.Fatalf("async update moved visible text from %q to %q", before, m.plain[m.vp.YOffset])
	}
	if m.notice != "" {
		t.Fatalf("unexpected rendering notice %q", m.notice)
	}
	m.vp.GotoTop()
	if !strings.Contains(m.View(), "\U0010eeee") {
		t.Fatal("diagram not drawn")
	}
	for _, line := range m.plain {
		if strings.Contains(line, "\U0010eeee") {
			t.Fatal("placeholder entered text data")
		}
	}
}

func TestFailureLeavesSourceAndSearchUsesUnwrappedSource(t *testing.T) {
	m := diagramModel(t, diagramMarkdown, func(context.Context, string, string) (*mermaid.Image, error) { return nil, errors.New("parse failed") })
	deadline := time.Now().Add(30 * time.Millisecond)
	for time.Now().Before(deadline) {
		m.mermaid.Poll()
		time.Sleep(time.Millisecond)
	}
	m.rerender()
	if len(m.diagrams) != 0 || !strings.Contains(m.rendered.Text(), "Needle") || m.notice != "" {
		t.Fatal("failure did not silently retain source")
	}
	m.cur.query = "A[Needle 世界] --> B"
	m.findMatches()
	if len(m.cur.matches) != 1 {
		t.Fatal("source search failed")
	}
	fallback := New("", []byte(diagramMarkdown), render.New(render.Mocha()), 0)
	mm, _ := fallback.Update(tea.WindowSizeMsg{Width: 25, Height: 10})
	fallback = mm.(Model)
	fallback.cur.query = "A[Needle 世界] --> B"
	fallback.findMatches()
	if len(fallback.cur.matches) != 1 || strings.Contains(fallback.View(), "\U0010eeee") {
		t.Fatal("fallback source search must ignore wrapping")
	}
}

func TestDiagramSearchSelectionMenuAndCopy(t *testing.T) {
	m := readyDiagrams(t, diagramModel(t, diagramMarkdown+"\n"+diagramMarkdown, successRender), 2)
	m.cur.query = "needle 世界"
	m.findMatches()
	if len(m.cur.matches) != 2 {
		t.Fatalf("source matches=%v", m.cur.matches)
	}
	m.startSearchFrom()
	if !m.rendered.Lines[m.vp.YOffset].Image {
		t.Fatal("search did not scroll to image")
	}
	original := m.View()
	m.sel = selection{active: true, a: pos{0, 0}, b: pos{5, 10}}
	if got := m.selectedText(); got != diagramSource {
		t.Fatalf("copy=%q", got)
	}
	if got := m.View(); got != original {
		t.Fatal("selection altered image ID colors")
	}
	copied := ""
	m.Copy = func(s string) error { copied = s; return nil }
	m.openMenu(10, 2)
	found := -1
	for i, item := range m.menu.items {
		if item.label == "Copy Mermaid source" {
			found = i
		}
	}
	if found < 0 {
		t.Fatal("missing diagram source menu action")
	}
	if !strings.Contains(ansi.Strip(m.View()), "Copy Mermaid source") {
		t.Fatal("menu not composited over diagram")
	}
	m.runMenuItem(found)
	if copied != diagramSource {
		t.Fatal("menu copied incorrect data")
	}
	m.openMenu(10, 2)
	m.menu.idx = 0
	m.runMenuItem(0)
	if m.viewer == nil {
		t.Fatal("open diagram menu failed")
	}
}

func TestViewerZoomPanCopyReturnAndMouse(t *testing.T) {
	m := readyDiagrams(t, diagramModel(t, "[link](https://example.com)\n\n"+diagramMarkdown, successRender), 1)
	m.focus = 1
	m.scrollTo(m.links[1].line)
	offset := m.vp.YOffset
	depth := len(m.stack)
	m, _ = press(m, "enter")
	if m.viewer == nil {
		t.Fatal("caption enter did not open viewer")
	}
	for i := 0; i < 50; i++ {
		m, _ = press(m, "+")
	}
	if m.viewer.zoom != 8 {
		t.Fatal("upper zoom bound")
	}
	for i := 0; i < 200; i++ {
		m, _ = press(m, "l")
		m, _ = press(m, "j")
	}
	c, r := m.viewerSize()
	if m.viewer.x != max(0, c-m.termW) || m.viewer.y != max(0, r-(m.termH-1)) {
		t.Fatal("pan bounds")
	}
	for i := 0; i < 100; i++ {
		m, _ = press(m, "-")
	}
	if m.viewer.zoom != .25 || m.viewer.x != 0 || m.viewer.y != 0 {
		t.Fatal("lower zoom/pan bounds")
	}
	m, _ = press(m, "0")
	if m.viewer.zoom != 1 {
		t.Fatal("reset")
	}
	m = mouse(m, 20, 5, tea.MouseActionPress, tea.MouseButtonWheelUp)
	if m.viewer.zoom != 1.25 {
		t.Fatal("wheel zoom")
	}
	m, _ = press(m, "+")
	m, _ = press(m, "+")
	m, _ = press(m, "+")
	m = mouse(m, 20, 5, tea.MouseActionPress, tea.MouseButtonLeft)
	x, y := m.viewer.x, m.viewer.y
	m = mouse(m, 15, 3, tea.MouseActionMotion, tea.MouseButtonLeft)
	if m.viewer.x < x || m.viewer.y < y {
		t.Fatal("drag direction")
	}
	m = mouse(m, 15, 3, tea.MouseActionRelease, tea.MouseButtonLeft)
	copied := ""
	m.Copy = func(s string) error { copied = s; return nil }
	m, _ = press(m, "y")
	if copied != diagramSource {
		t.Fatal("viewer copy")
	}
	m, _ = press(m, "q")
	if m.viewer != nil || m.vp.YOffset != offset || m.focus != 1 || len(m.stack) != depth {
		t.Fatal("viewer return lost document state")
	}
	m.vp.GotoTop()
	m = click(m, 10, 4)
	if m.viewer == nil {
		t.Fatal("click image did not open")
	}
	m, _ = press(m, "esc")
	if m.viewer != nil {
		t.Fatal("escape did not close")
	}
	m.openDiagram(1)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c must quit entire app")
	}
}

func TestResizePartialVisibilityAndRepeatedViewerDoNotRerender(t *testing.T) {
	var calls atomic.Int32
	m := readyDiagrams(t, diagramModel(t, "> - "+strings.ReplaceAll(strings.TrimSuffix(diagramMarkdown, "\n"), "\n", "\n>   ")+"\n", func(context.Context, string, string) (*mermaid.Image, error) { calls.Add(1); return testImage(), nil }), 1)
	d := m.diagrams[1]
	if d.cols != 74 {
		t.Fatalf("nested width=%d", d.cols)
	}
	m.vp.SetYOffset(8)
	view := m.View()
	if !strings.Contains(view, "\U0010eeee") {
		t.Fatal("partially visible image missing")
	}
	for i := 0; i < 10; i++ {
		m.openDiagram(1)
		m, _ = press(m, "+")
		m, _ = press(m, "q")
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 44, Height: 12})
	m = mm.(Model)
	if m.diagrams[1].cols != 38 || calls.Load() != 1 {
		t.Fatal("resize did not relayout cached image")
	}
	m.openDiagram(1)
	mm, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 8})
	m = mm.(Model)
	if m.viewer == nil || calls.Load() != 1 {
		t.Fatal("viewer resize rerendered")
	}
}

func TestDiagramsNavigationBreadcrumbReloadAndStaleResults(t *testing.T) {
	dir := t.TempDir()
	root, child, other := filepath.Join(dir, "root.md"), filepath.Join(dir, "child.md"), filepath.Join(dir, "other.md")
	for _, p := range []string{root, child, other} {
		if err := os.WriteFile(p, []byte(diagramMarkdown+"\n"+strings.Repeat("tail\n\n", 30)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var calls atomic.Int32
	m := diagramModel(t, diagramMarkdown+"\n"+strings.Repeat("tail\n\n", 30), func(context.Context, string, string) (*mermaid.Image, error) { calls.Add(1); return testImage(), nil })
	m.cur = newDoc(root, m.cur.src)
	m.rerender()
	m = readyDiagrams(t, m, 1)
	m.vp.SetYOffset(10)
	offset := m.vp.YOffset
	m.push(child, "")
	m = readyDiagrams(t, m, 1)
	m.push(other, "")
	m = readyDiagrams(t, m, 1)
	if len(m.stack) != 2 || !m.restore(0) || m.cur.path != root || m.vp.YOffset != offset {
		t.Fatal("breadcrumb restore lost diagram state")
	}
	// Cached results should cover all three paths with identical source.
	count := calls.Load()
	m.push(child, "")
	m.restore(0)
	if calls.Load() != count {
		t.Fatal("navigation rerendered identical source")
	}
	m.openDiagram(1)
	m.cur.lastMod = time.Time{}
	if err := os.WriteFile(root, []byte("# Replaced\n\nNo diagram."), 0600); err != nil {
		t.Fatal(err)
	}
	mm, _ := m.Update(tickMsg(time.Now()))
	m = mm.(Model)
	if m.viewer != nil || len(m.diagrams) != 0 || strings.Contains(m.View(), "\U0010eeee") {
		t.Fatal("reload retained stale diagram/viewer")
	}
}

func TestFirstImageUploadSurvivesMenuClipping(t *testing.T) {
	m := readyDiagrams(t, diagramModel(t, diagramMarkdown, func(context.Context, string, string) (*mermaid.Image, error) {
		var b bytes.Buffer
		_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1600, 2)))
		return &mermaid.Image{PNG: b.Bytes(), Width: 1600, Height: 2}, nil
	}), 1)
	var out bytes.Buffer
	writer := graphics.NewWriter(&out)
	defer writer.Close()
	m.graphics = writer
	m.rerender()
	m.openMenu(0, 0)
	frame := m.View()
	if !strings.Contains(frame, "\U0010eeee") {
		t.Fatal("test must leave part of image uncovered")
	}
	_, _ = writer.Write([]byte(frame))
	if !strings.Contains(out.String(), "f=100") {
		t.Fatal("overlay hid first upload marker but left visible placeholders")
	}
}
