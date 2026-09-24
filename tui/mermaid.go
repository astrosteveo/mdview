package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/astrosteveo/mdview/graphics"
	"github.com/astrosteveo/mdview/mermaid"
	"github.com/astrosteveo/mdview/render"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const diagramScheme = "mdview-mermaid:"

type diagram struct {
	block          *render.MermaidBlock
	image          *mermaid.Image
	id, cols, rows int
}
type diagramView struct {
	diagram        diagram
	zoom           float64
	x, y           int
	dragging       bool
	mouseX, mouseY int
	focus          int
}

// EnableMermaid is only called after a successful pre-pager capability probe.
func (m *Model) EnableMermaid(session *mermaid.Session, writer *graphics.Writer, cap graphics.Capability) {
	m.mermaid = session
	m.graphics = writer
	m.cells = cap
}
func (m *Model) diagramLines(doc render.Doc) render.Doc {
	m.diagrams = map[int]diagram{}
	if m.mermaid == nil {
		return doc
	}
	sources := []string{}
	seen := map[int]bool{}
	for _, l := range doc.Lines {
		if b := l.Mermaid; b != nil && !seen[b.ID] {
			seen[b.ID] = true
			sources = append(sources, b.Source)
		}
	}
	m.mermaid.Sync(m.cur.path+"\x00"+string(m.cur.src), sources)
	var images []*mermaid.Image
	for _, src := range sources {
		if img := m.mermaid.Get(src); img != nil {
			images = append(images, img)
		}
	}
	m.graphics.Use(images)
	var lines []render.Line
	for i := 0; i < len(doc.Lines); {
		l := doc.Lines[i]
		b := l.Mermaid
		if b == nil {
			lines = append(lines, l)
			i++
			continue
		}
		end := i + 1
		for end < len(doc.Lines) && doc.Lines[end].Mermaid == b {
			end++
		}
		img := m.mermaid.Get(b.Source)
		if img == nil {
			lines = append(lines, doc.Lines[i:end]...)
			i = end
			continue
		}
		cols, rows := graphics.FitInline(img, max(1, min(b.Width, m.termW-marginX*2-l.Indent)), m.cells.CellWidth, m.cells.CellHeight)
		d := diagram{b, img, m.graphics.ID(img), cols, rows}
		m.diagrams[b.ID] = d
		prefix := ansi.Cut(l.Text, 0, l.Indent)
		// Later prefixes may be hanging list indentation rather than the bullet.
		continuation := prefix
		if end > i+1 {
			continuation = ansi.Cut(doc.Lines[i+1].Text, 0, l.Indent)
		}
		for y := 0; y < rows; y++ {
			p := continuation
			if y == 0 {
				p = prefix
			}
			lines = append(lines, render.Line{Text: p + strings.Repeat(" ", cols), Mermaid: b, Indent: l.Indent, BaseRow: l.BaseRow, Image: true, ImageRow: y})
		}
		captionText := "Mermaid · enter to view"
		if cols < 22 {
			captionText = "View diagram"
		}
		caption := ansi.Truncate(captionText, cols, "")
		lines = append(lines, render.Line{Text: continuation + caption, Mermaid: b, Indent: l.Indent, BaseRow: l.BaseRow,
			Links: []render.Link{{Start: l.Indent, End: l.Indent + ansi.StringWidth(caption), Dest: diagramScheme + strconv.Itoa(b.ID)}}})
		i = end
	}
	anchors := map[string]int{}
	for name, old := range doc.Anchors {
		for i, l := range lines {
			if l.BaseRow == old {
				anchors[name] = i
				break
			}
		}
	}
	return render.Doc{Lines: lines, Anchors: anchors}
}
func (m *Model) openDiagram(id int) {
	d, ok := m.diagrams[id]
	if !ok {
		return
	}
	m.viewer = &diagramView{diagram: d, zoom: 1, focus: m.focus}
	m.hover = nil
	m.menu = nil
}
func (m Model) diagramAt(p pos) (diagram, bool) {
	if p.line < 0 || p.line >= len(m.rendered.Lines) {
		return diagram{}, false
	}
	l := m.rendered.Lines[p.line]
	if l.Mermaid == nil {
		return diagram{}, false
	}
	d, ok := m.diagrams[l.Mermaid.ID]
	return d, ok && p.col >= l.Indent && p.col < l.Indent+d.cols
}
func (m Model) viewerSize() (int, int) {
	v := m.viewer
	c, r := graphics.Fit(v.diagram.image, max(1, m.termW), max(1, m.termH-1), m.cells.CellWidth, m.cells.CellHeight)
	return max(1, int(math.Round(float64(c)*v.zoom))), max(1, int(math.Round(float64(r)*v.zoom)))
}
func (m *Model) clampPan() {
	c, r := m.viewerSize()
	m.viewer.x = max(0, min(m.viewer.x, c-m.termW))
	m.viewer.y = max(0, min(m.viewer.y, r-max(1, m.termH-1)))
}
func (m *Model) zoomDiagram(factor float64) {
	oldC, oldR := m.viewerSize()
	v := m.viewer
	v.zoom = math.Max(.25, math.Min(8, v.zoom*factor))
	c, r := m.viewerSize()
	v.x = int(float64(v.x+m.termW/2)*float64(c)/float64(oldC)) - m.termW/2
	v.y = int(float64(v.y+(m.termH-1)/2)*float64(r)/float64(oldR)) - (m.termH-1)/2
	m.clampPan()
}
func (m Model) updateViewer(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Model is a value; copy viewer state before editing it.
	v := *m.viewer
	m.viewer = &v
	switch e := msg.(type) {
	case tea.KeyMsg:
		switch e.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc", "q":
			m.focus = v.focus
			m.viewer = nil
			return m, nil
		case "+", "=":
			m.zoomDiagram(1.25)
		case "-":
			m.zoomDiagram(1 / 1.25)
		case "0":
			v.zoom = 1
			v.x = 0
			v.y = 0
		case "left", "h":
			v.x -= 3
		case "right", "l":
			v.x += 3
		case "up", "k":
			v.y -= 2
		case "down", "j":
			v.y += 2
		case "y":
			m.copyText(v.diagram.block.Source)
		}
	case tea.MouseMsg:
		switch {
		case e.Button == tea.MouseButtonWheelUp:
			m.zoomDiagram(1.25)
		case e.Button == tea.MouseButtonWheelDown:
			m.zoomDiagram(1 / 1.25)
		case e.Action == tea.MouseActionPress && e.Button == tea.MouseButtonLeft:
			v.dragging = true
			v.mouseX = e.X
			v.mouseY = e.Y
		case e.Action == tea.MouseActionRelease:
			v.dragging = false
		case e.Action == tea.MouseActionMotion && v.dragging:
			v.x += v.mouseX - e.X
			v.y += v.mouseY - e.Y
			v.mouseX = e.X
			v.mouseY = e.Y
		}
	}
	m.clampPan()
	return m, nil
}
func (m Model) viewerView() string {
	v := m.viewer
	cols, rows := m.viewerSize()
	height := max(1, m.termH-1)
	left, top := max(0, (m.termW-cols)/2), max(0, (height-rows)/2)
	out := make([]string, height+1)
	for y := 0; y < height; y++ {
		out[y] = "\x1b[K"
		iy := y - top + v.y
		if iy < 0 || iy >= rows {
			continue
		}
		out[y] += strings.Repeat(" ", left) + m.graphics.Row(v.diagram.id, v.diagram.image, cols, rows, iy, v.x, min(cols, v.x+m.termW))
	}
	footer := fmt.Sprintf("Mermaid %.0f%% · +/- zoom · arrows/hjkl pan · drag/wheel · 0 reset · y source · esc/q back", v.zoom*100)
	if m.termW < 100 {
		footer = fmt.Sprintf("%.0f%% · +/- zoom · hjkl pan · 0 fit · y copy · q back", v.zoom*100)
	}
	if m.termW < 52 {
		footer = fmt.Sprintf("%.0f%% +/- · hjkl · 0 fit · y copy · q back", v.zoom*100)
	}
	if m.notice != "" && m.termW >= 100 {
		footer = m.notice + " · " + footer
	}
	out[height] = ansi.Truncate(footer, m.termW, "") + m.osc52
	return strings.Join(out, "\n")
}
