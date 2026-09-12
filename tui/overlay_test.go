package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/astrosteveo/mdview/render"
)

func TestHighlightKeepsSurroundingStyleAndWidth(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	row := "\x1b[Kab\x1b[1;31mcdef\x1b[0mgh"
	sel := lipgloss.NewStyle().Reverse(true)
	out := highlight(row, 3, 5, sel)
	if ansi.Strip(out) != "abcdefgh" || ansi.StringWidth(out) != 8 {
		t.Fatalf("text/width changed: %q", out)
	}
	if !strings.HasPrefix(out, "\x1b[K") {
		t.Fatalf("pre-erase prefix lost: %q", out)
	}
	if !strings.Contains(out, "\x1b[7mde\x1b[0m") {
		t.Fatalf("selection not applied: %q", out)
	}
	// Highlight past the end pads with spaces.
	if got := ansi.Strip(highlight("ab", 1, 5, sel)); got != "ab   " {
		t.Fatalf("pad = %q", got)
	}
}

func TestSpliceOverlaysBox(t *testing.T) {
	row := "0123456789"
	out := splice(row, 3, "[box]")
	if ansi.Strip(out) != "012[box]89" {
		t.Fatalf("splice = %q", ansi.Strip(out))
	}
	if got := ansi.Strip(splice("ab", 4, "XY")); got != "ab  XY" {
		t.Fatalf("splice past end = %q", got)
	}
}

func TestFlattenScaledHeading(t *testing.T) {
	r := render.New(render.Mocha())
	r.BigHeadings = true
	d := r.RenderDoc([]byte("# Title\n\ntext"), 40)
	rows := []string{preclear(d.Lines[0].Text) + d.Lines[0].Text, d.Lines[1].Text, d.Lines[2].Text}
	if !render.IsScaled(rows[0]) || !render.IsFiller(rows[1]) {
		t.Fatal("fixture is not a scaled heading + filler")
	}
	flatten(rows, 1) // hitting the filler flattens its heading too
	if render.IsScaled(rows[0]) || rows[1] != "\x1b[K" || ansi.Strip(rows[0]) != "Title" {
		t.Fatalf("flatten: %q / %q", rows[0], rows[1])
	}
}
