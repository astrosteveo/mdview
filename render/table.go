package render

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"strings"
)

// table lays out a GFM table with box-drawing borders. Cells are rendered as
// single styled strings; lipgloss/table handles column sizing and wrapping
// when the natural width exceeds the available width.
func (r *Renderer) table(t *east.Table, width int) []string {
	var headers []string
	var rows [][]string
	var aligns []east.Alignment

	cellText := func(c ast.Node) string {
		return r.renderLine(trimRight(r.inlines(c)))
	}

	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []string
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			cell, ok := c.(*east.TableCell)
			if !ok {
				continue
			}
			cells = append(cells, cellText(cell))
			if _, isHeader := row.(*east.TableHeader); isHeader {
				aligns = append(aligns, cell.Alignment)
			}
		}
		if _, isHeader := row.(*east.TableHeader); isHeader {
			headers = cells
		} else {
			rows = append(rows, cells)
		}
	}

	th := r.th
	border := lipgloss.NewStyle().Foreground(th.Table)
	headStyle := lipgloss.NewStyle().Foreground(th.Table).Bold(true).Padding(0, 1)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)

	build := func() *table.Table {
		return table.New().
			Border(lipgloss.RoundedBorder()).
			BorderStyle(border).
			BorderRow(false).
			Headers(headers...).
			Rows(rows...).
			StyleFunc(func(row, col int) lipgloss.Style {
				s := cellStyle
				if row == table.HeaderRow {
					s = headStyle
				}
				if col < len(aligns) {
					switch aligns[col] {
					case east.AlignRight:
						s = s.Align(lipgloss.Right)
					case east.AlignCenter:
						s = s.Align(lipgloss.Center)
					}
				}
				return s
			})
	}

	rendered := build().String()
	if lipgloss.Width(rendered) > width {
		rendered = build().Width(width).String()
	}

	lines := strings.Split(rendered, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}
