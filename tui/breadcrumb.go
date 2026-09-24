package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	crumbInactive = -1
	crumbOverflow = -2
	crumbBack     = -3
)

type crumb struct {
	text             string
	x, width, target int
}

type breadcrumbLayout struct {
	parts  []crumb
	hidden int // stack entries [0, hidden) are in the overflow menu
}

// breadcrumb computes both drawing and hit regions in terminal cells.
func (m Model) breadcrumb() breadcrumbLayout {
	var l breadcrumbLayout
	w := max(m.termW, 0)
	add := func(text string, target int) {
		x := 0
		if n := len(l.parts); n > 0 {
			p := l.parts[n-1]
			x = p.x + p.width
		}
		l.parts = append(l.parts, crumb{text, x, ansi.StringWidth(text), target})
	}
	close := " ✕ "
	if w < closeW {
		close = ansi.Truncate("✕", w, "")
	}
	available := w - ansi.StringWidth(close)
	n := len(m.stack)
	current := m.cur.name()
	// Reserve the overflow affordance before truncating an oversized current name.
	reserve := 0
	if n > 0 {
		reserve = min(4, available)
	}
	current = ansi.Truncate(current, max(available-reserve, 0), "…")
	used := ansi.StringWidth(current)
	start := n
	for start > 0 {
		cost := ansi.StringWidth(m.stack[start-1].name()) + 3
		overflow := 0
		if start > 1 {
			overflow = 4
		}
		if used+cost+overflow > available {
			break
		}
		used += cost
		start--
	}
	l.hidden = start
	if start > 0 && available > 0 {
		add("…", crumbOverflow)
		if available >= 4 {
			add(" › ", crumbInactive)
		}
	}
	for i := start; i < n; i++ {
		add(m.stack[i].name(), i)
		add(" › ", crumbInactive)
	}
	add(current, crumbInactive)
	end := 0
	if len(l.parts) > 0 {
		p := l.parts[len(l.parts)-1]
		end = p.x + p.width
	}
	add(strings.Repeat(" ", max(available-end, 0)), crumbInactive)
	add(close, crumbBack)
	return l
}

func (m Model) header() string {
	th := m.renderer.Theme()
	bar := lipgloss.NewStyle().Background(th.Mantle).Foreground(th.Muted)
	var out strings.Builder
	for _, p := range m.breadcrumb().parts {
		style := bar
		switch {
		case p.target == crumbBack:
			style = bar.Background(th.Quote).Foreground(th.Mantle).Bold(true)
		case p.target == crumbOverflow || p.target >= 0:
			style = bar.Foreground(th.Link).Underline(true)
		case p.text != " › " && strings.TrimSpace(p.text) != "":
			style = bar.Foreground(th.Text).Bold(true)
		}
		out.WriteString(style.Render(p.text))
	}
	return out.String()
}

func (m *Model) clickBreadcrumb(x int) {
	l := m.breadcrumb()
	for _, p := range l.parts {
		if x < p.x || x >= p.x+p.width {
			continue
		}
		switch {
		case p.target >= 0:
			m.restore(p.target)
		case p.target == crumbBack:
			m.pop()
		case p.target == crumbOverflow:
			items := make([]menuItem, l.hidden)
			for i := range items {
				index := i
				items[i] = menuItem{m.stack[i].name(), func(m *Model) tea.Cmd { m.restore(index); return nil }}
			}
			m.clearSelection()
			m.hover = nil
			m.menu = &menu{x: p.x, y: 1, items: items}
		}
		return
	}
}
