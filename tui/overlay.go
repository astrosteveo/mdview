package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/astrosteveo/mdview/render"
)

// Row compositing for selections, tooltips and menus. Rows are styled
// strings; all cuts are ANSI-aware (x/ansi re-opens SGR state on each side
// of a cut, so pieces can be re-joined freely).

// padTo extends row with spaces to at least n cells.
func padTo(row string, n int) string {
	if w := ansi.StringWidth(row); w < n {
		return row + strings.Repeat(" ", n-w)
	}
	return row
}

// highlight restyles cells [from, to) of row with sty, dropping any styling
// the range had (a flat selection colour, like every editor).
func highlight(row string, from, to int, sty lipgloss.Style) string {
	if to <= from {
		return row
	}
	row = padTo(row, to)
	left := ansi.Cut(row, 0, from)
	mid := ansi.Strip(ansi.Cut(row, from, to))
	right := ansi.Cut(row, to, ansi.StringWidth(row))
	return left + sty.Render(mid) + right
}

// splice draws box (a single styled row of width w) over row starting at
// cell x, keeping whatever lies to the right of it.
func splice(row string, x int, box string) string {
	w := ansi.StringWidth(box)
	row = padTo(row, x+w)
	left := ansi.Cut(row, 0, x)
	right := ansi.Cut(row, x+w, ansi.StringWidth(row))
	return left + box + right
}

// flatten turns a kitty-scaled heading at rows[i] (or the filler under it)
// into its 1x form for this frame so a cell-addressed overlay lines up.
// The heading keeps its pre-erase prefix, which also clears the filler row.
func flatten(rows []string, i int) {
	if i < 0 || i >= len(rows) {
		return
	}
	if render.IsFiller(rows[i]) {
		i--
		if i < 0 {
			return
		}
	}
	if !render.IsScaled(rows[i]) {
		return
	}
	rows[i] = render.Unscale(rows[i])
	if i+1 < len(rows) && render.IsFiller(rows[i+1]) {
		rows[i+1] = "\x1b[K"
	}
}
