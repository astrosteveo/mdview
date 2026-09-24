package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func trail(t *testing.T, count int) Model {
	m, _ := setup(t)
	src := []byte(strings.Repeat("needle paragraph\n\n", 60))
	m.cur.src = src
	for i := 0; i < count; i++ {
		d := newDoc(fmt.Sprintf("/history/%d/same.md", i), src)
		d.query, d.matchIdx, d.yOffset = "needle", i, i+2
		m.stack = append(m.stack, d)
	}
	m.cur.path = "/current/same.md"
	m.layout()
	m.rerender()
	return m
}

func crumbX(t *testing.T, m Model, target int) int {
	t.Helper()
	for _, p := range m.breadcrumb().parts {
		if p.target == target && p.width > 0 {
			return p.x
		}
	}
	t.Fatalf("missing target %d in %q", target, ansi.Strip(m.header()))
	return 0
}

func TestBreadcrumbRestoresEveryHistoryPosition(t *testing.T) {
	for i := 0; i < 5; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			m := trail(t, 5)
			// Repeat visits to the same path still retain separate state.
			m.stack[3].path = m.stack[1].path
			want := m.stack[i]
			m.searching = true
			m.search.SetValue("unfinished")
			m.search.Focus()
			m.sel = selection{active: true, dragging: true}
			m.hover = &linkRef{}
			m.focus = 0
			m.lastClick = time.Now()
			m = click(m, crumbX(t, m, i), 0)
			if m.cur.path != want.path || len(m.stack) != i || m.vp.YOffset != want.yOffset || m.cur.query != want.query || m.cur.matchIdx != want.matchIdx || len(m.cur.matches) != 60 {
				t.Fatalf("wrong restored state: %+v offset=%d", m.cur, m.vp.YOffset)
			}
			if m.searching || m.search.Focused() || m.search.Value() != "" || m.sel.active || m.sel.dragging || m.hover != nil || m.focus != -1 || m.menu != nil || !m.lastClick.IsZero() {
				t.Fatal("temporary state survived navigation")
			}
		})
	}
}

func TestBreadcrumbRootAndNewTrail(t *testing.T) {
	m, dir := setup(t)
	m.follow("sub/child.md")
	m.follow("../root.md")
	m = click(m, crumbX(t, m, 0), 0)
	if m.headerH() != 0 || len(m.stack) != 0 {
		t.Fatal("root retained header")
	}
	m.follow("sub/child.md")
	if len(m.stack) != 1 || m.stack[0].path != filepath.Join(dir, "root.md") {
		t.Fatal("new trail retained forward history")
	}
}

func TestBreadcrumbInactiveCellsAndBack(t *testing.T) {
	for x := 0; x < 80; x++ {
		m := trail(t, 5)
		target := crumbInactive
		for _, p := range m.breadcrumb().parts {
			if x >= p.x && x < p.x+p.width {
				target = p.target
			}
		}
		if target != crumbInactive {
			continue
		}
		m = click(m, x, 0)
		if len(m.stack) != 5 || m.menu != nil {
			t.Fatalf("inactive cell %d navigated", x)
		}
	}
	for _, k := range []tea.KeyType{tea.KeyEsc, tea.KeyBackspace} {
		m := trail(t, 5)
		mm, _ := m.Update(tea.KeyMsg{Type: k})
		if len(mm.(Model).stack) != 4 {
			t.Fatal("keyboard back failed")
		}
	}
	m := trail(t, 5)
	m = click(m, crumbX(t, m, crumbBack), 0)
	if len(m.stack) != 4 {
		t.Fatal("close did not step back once")
	}
}

func TestBreadcrumbWidthsResizeAndOverflow(t *testing.T) {
	for width := 1; width <= 160; width++ {
		m := trail(t, 5)
		m.cur.path = "/current/界面é👩‍💻" + strings.Repeat("long", 20) + ".md"
		m.stack[0].path = "/old/界面.md"
		mm, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 8})
		m = mm.(Model)
		if got := ansi.StringWidth(m.header()); got != width || strings.Contains(m.header(), "\n") {
			t.Fatalf("width %d: got %d %q", width, got, m.header())
		}
		if width >= 4 && m.breadcrumb().hidden > 0 {
			hidden := m.breadcrumb().hidden
			m = click(m, crumbX(t, m, crumbOverflow), 0)
			if m.menu == nil || len(m.menu.items) != hidden {
				t.Fatalf("width %d lost ancestors", width)
			}
			rows, x, y := m.menuBox()
			for _, r := range rows {
				if x+ansi.StringWidth(r) > width {
					t.Fatalf("menu overflows at %d", width)
				}
			}
			if y+len(rows) > 8 {
				t.Fatal("menu too tall")
			}
			m, _ = press(m, "esc")
			if m.menu != nil || len(m.stack) != 5 {
				t.Fatal("esc navigated")
			}
		}
	}
	// Unicode preceding a destination uses cells, not bytes or runes.
	m := trail(t, 3)
	m.stack[0].path = "/界é.md"
	header := ansi.Strip(m.header())
	prefix, _, _ := strings.Cut(header, "same.md")
	m = click(m, ansi.StringWidth(prefix), 0)
	if len(m.stack) != 1 {
		t.Fatal("Unicode shifted hit region")
	}
}

func TestOverflowAllEntriesReachable(t *testing.T) {
	for _, height := range []int{1, 2, 3, 6} {
		for target := 0; target < 25; target++ {
			m := trail(t, 25)
			m.termW = 18
			m.termH = height
			m = click(m, crumbX(t, m, crumbOverflow), 0)
			if m.menu == nil || len(m.menu.items) != 25 {
				t.Fatal("missing overflow entries")
			}
			// Resize an open menu; it must remain usable.
			mm, _ := m.Update(tea.WindowSizeMsg{Width: 16, Height: height})
			m = mm.(Model)
			for j := 0; j < target; j++ {
				m, _ = press(m, "j")
			}
			rows, x, y := m.menuBox()
			if len(rows) > height {
				t.Fatal("menu exceeds terminal rows")
			}
			start, _, edge := m.menuWindow()
			if target%2 == 0 {
				m, _ = press(m, "enter")
			} else {
				// Selected entry is at the end of the visible window.
				m = click(m, x+edge, y+edge+target-start)
			}
			if len(m.stack) != target || m.cur.yOffset != target+2 {
				t.Fatalf("height %d target %d selected wrong entry", height, target)
			}
		}
	}
	m := trail(t, 25)
	m.termW = 18
	m.termH = 5
	m = click(m, crumbX(t, m, crumbOverflow), 0)
	for i := 0; i < 24; i++ {
		m = mouse(m, 2, 2, tea.MouseActionPress, tea.MouseButtonWheelDown)
	}
	if m.menu == nil || m.menu.idx != 24 {
		t.Fatal("wheel did not scroll menu")
	}
	m, _ = press(m, "j")
	if m.menu.idx != 0 {
		t.Fatal("keyboard did not wrap")
	}
	m, _ = press(m, "k")
	if m.menu.idx != 24 {
		t.Fatal("reverse wrap failed")
	}
}
