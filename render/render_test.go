package render

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWrapRespectsWidthAndStyles(t *testing.T) {
	spans := []span{
		{"The quick ", attrs{}},
		{"brown fox", attrs{bold: true}},
		{" jumps over the lazy dog", attrs{}},
	}
	for _, line := range wrap(spans, 12) {
		if w := lineWidth(line); w > 12 {
			t.Fatalf("line %q is %d cells wide", plainText(line), w)
		}
		if len(line) > 0 && strings.HasPrefix(line[0].text, " ") {
			t.Fatalf("line %q starts with a space", plainText(line))
		}
	}
	// Every bold word must keep its attribute after wrapping, at width 12
	// ("The quick" / "brown fox" / ...) and at width 7, where it splits.
	for _, width := range []int{12, 7} {
		var bold []string
		for _, line := range wrap(spans, width) {
			for _, sp := range line {
				if sp.a.bold {
					bold = append(bold, sp.text)
				}
			}
		}
		if got := strings.Join(bold, " "); got != "brown fox" {
			t.Fatalf("width %d: bold text = %q, want %q", width, got, "brown fox")
		}
	}
}

func TestWrapHardSplitsLongLines(t *testing.T) {
	lines := wrapHard([]span{{strings.Repeat("x", 25) + "\nshort", attrs{}}}, 10)
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(lines))
	}
	if plainText(lines[3]) != "short" {
		t.Fatalf("last line = %q", plainText(lines[3]))
	}
}

func TestRenderSample(t *testing.T) {
	src, err := os.ReadFile("../testdata/sample.md")
	if err != nil {
		t.Skip("sample not found")
	}
	out := ansi.Strip(New(Mocha()).Render(src, 60))
	for _, want := range []string{
		"mdview", "[✓] Parse CommonMark", "┃ A blockquote", "╭───", "hello, mdview",
		"(https://github.com/example/mdview)", "▨ A diagram",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > 60 {
			t.Errorf("line exceeds width (%d): %q", w, line)
		}
	}
}
