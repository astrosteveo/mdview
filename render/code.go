package render

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// codeBlock renders a fenced or indented block: an optional language chip on
// the first line, then syntax-highlighted lines on a padded background.
// Long lines are hard-wrapped rather than truncated.
func (r *Renderer) codeBlock(lang, code string, width int) []string {
	code = strings.ReplaceAll(code, "\t", "    ")
	const padL, padR = 1, 1
	inner := width - padL - padR
	if inner < 1 {
		inner = 1
	}

	bg := lipgloss.NewStyle().Background(r.th.Surface0)
	fill := func(s string) string {
		w := ansi.StringWidth(s)
		return bg.Render(strings.Repeat(" ", padL)) + s + bg.Render(strings.Repeat(" ", max(0, inner-w)+padR))
	}

	var out []string
	if lang != "" {
		chip := lipgloss.NewStyle().
			Foreground(r.th.CodeLabel).
			Background(r.th.Surface1).
			Bold(true).
			Render(" " + lang + " ")
		out = append(out, fill(chip))
	}

	spans := r.highlight(lang, code)
	lines := wrapHard(spans, inner)
	for _, line := range lines {
		out = append(out, fill(r.renderLine(line)))
	}
	if lang == "" && len(out) == 0 {
		out = append(out, fill(""))
	}
	return out
}

// highlight tokenises code with Chroma and maps token colours onto spans.
func (r *Renderer) highlight(lang, code string) []span {
	base := attrs{codeBlock: true}

	var lexer chroma.Lexer
	if lang != "" {
		lexer = lexers.Get(lang)
	}
	if lexer == nil {
		return []span{{code, base}}
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get(r.th.ChromaStyle)
	if style == nil {
		style = styles.Fallback
	}

	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return []span{{code, base}}
	}

	var out []span
	for tok := it(); tok != chroma.EOF; tok = it() {
		a := base
		entry := style.Get(tok.Type)
		if entry.Colour.IsSet() {
			a.fg = entry.Colour.String()
		}
		a.bold = entry.Bold == chroma.Yes
		a.italic = entry.Italic == chroma.Yes
		a.underline = entry.Underline == chroma.Yes
		out = append(out, span{tok.Value, a})
	}
	return out
}
