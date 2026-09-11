// Package render turns Markdown into styled terminal text.
//
// The pipeline is: goldmark parses to an AST; block nodes are rendered to
// lines recursively (each container narrows the width and prefixes its
// children); inline nodes become styled spans that are word-wrapped before
// any ANSI is emitted, so styles survive line breaks and indentation.
package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

type Renderer struct {
	// NoURLs hides link/image destinations after their text.
	NoURLs bool

	th         Theme
	src        []byte
	styleCache map[attrs]lipgloss.Style

	quoteDepth   int
	listDepth    int
	skipCheckbox bool
}

func New(th Theme) *Renderer {
	return &Renderer{th: th, styleCache: map[attrs]lipgloss.Style{}}
}

func (r *Renderer) Theme() Theme { return r.th }

var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// Render renders markdown source to styled text wrapped at width.
func (r *Renderer) Render(src []byte, width int) string {
	if width < 20 {
		width = 20
	}
	r.src = src
	r.quoteDepth, r.listDepth, r.skipCheckbox = 0, 0, false
	doc := md.Parser().Parse(text.NewReader(src))
	return strings.Join(r.blocks(doc, width, false), "\n")
}

// blocks renders each child of parent, separating them with a blank line
// unless the container is a tight list item.
func (r *Renderer) blocks(parent ast.Node, width int, tight bool) []string {
	var out []string
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		lines := r.block(c, width, tight)
		if len(lines) == 0 {
			continue
		}
		if len(out) > 0 && !tight {
			out = append(out, "")
		}
		out = append(out, lines...)
	}
	return out
}

func (r *Renderer) block(n ast.Node, width int, tight bool) []string {
	switch v := n.(type) {
	case *ast.Heading:
		return r.heading(v, width)
	case *ast.Paragraph, *ast.TextBlock:
		return r.renderLines(r.inlines(v), width)
	case *ast.Blockquote:
		return r.blockquote(v, width)
	case *ast.List:
		return r.list(v, width)
	case *ast.FencedCodeBlock:
		return r.codeBlock(string(v.Language(r.src)), r.linesOf(v), width)
	case *ast.CodeBlock:
		return r.codeBlock("", r.linesOf(v), width)
	case *ast.HTMLBlock:
		return r.htmlBlock(v, width)
	case *ast.ThematicBreak:
		return []string{r.rule("─", width, r.th.Rule)}
	case *east.Table:
		return r.table(v, width)
	default:
		return r.blocks(v, width, tight)
	}
}

func (r *Renderer) heading(h *ast.Heading, width int) []string {
	level := h.Level
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	color := r.th.Heading[level-1]
	base := lipgloss.NewStyle().Foreground(color).Bold(true)
	if level >= 5 {
		base = base.Italic(true)
	}
	spans := r.inlines(h)
	// Headings are rendered as plain runs so the heading colour wins, but
	// inline code inside them keeps its chip.
	var out []string
	for _, line := range wrap(spans, width) {
		var sb strings.Builder
		for _, sp := range line {
			if sp.a.code {
				sb.WriteString(r.styleFor(sp.a).Render(sp.text))
			} else {
				sb.WriteString(base.Render(sp.text))
			}
		}
		out = append(out, sb.String())
	}
	switch level {
	case 1:
		out = append(out, r.rule("━", width, color))
	case 2:
		out = append(out, r.rule("─", width, r.th.Rule))
	}
	return out
}

func (r *Renderer) rule(ch string, width int, color lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(color).Render(strings.Repeat(ch, width))
}

func (r *Renderer) blockquote(q *ast.Blockquote, width int) []string {
	r.quoteDepth++
	inner := r.blocks(q, width-2, false)
	r.quoteDepth--
	bar := lipgloss.NewStyle().Foreground(r.th.Quote).Render("┃")
	for i, l := range inner {
		if l == "" {
			inner[i] = bar
		} else {
			inner[i] = bar + " " + l
		}
	}
	return inner
}

var bullets = []string{"•", "◦", "▪", "▫"}

func (r *Renderer) list(l *ast.List, width int) []string {
	bulletStyle := lipgloss.NewStyle().Foreground(r.th.Bullet)
	numStyle := lipgloss.NewStyle().Foreground(r.th.Number)

	// Compute marker width up front so every item aligns.
	count := 0
	for c := l.FirstChild(); c != nil; c = c.NextSibling() {
		count++
	}
	markerW := 2 // "• "
	if l.IsOrdered() {
		markerW = len(fmt.Sprintf("%d.", l.Start+count-1)) + 1
	}
	hang := markerW

	var out []string
	num := l.Start
	r.listDepth++
	for item := l.FirstChild(); item != nil; item = item.NextSibling() {
		var marker string
		if checked, isTask := startsWithCheckbox(item); isTask {
			r.skipCheckbox = true
			cb := checkboxSpan(checked)
			marker = r.styleFor(cb.a).Render(cb.text)
			hang = ansi.StringWidth(cb.text)
		} else if l.IsOrdered() {
			s := fmt.Sprintf("%d.", num)
			marker = numStyle.Render(s) + strings.Repeat(" ", markerW-len(s))
			hang = markerW
		} else {
			b := bullets[(r.listDepth-1)%len(bullets)]
			marker = bulletStyle.Render(b) + " "
			hang = markerW
		}
		num++

		lines := r.blocks(item, width-hang, l.IsTight)
		r.skipCheckbox = false
		if len(lines) == 0 {
			lines = []string{""}
		}
		pad := strings.Repeat(" ", hang)
		for i, ln := range lines {
			if i == 0 {
				out = append(out, marker+ln)
			} else {
				out = append(out, pad+ln)
			}
		}
		if !l.IsTight && item.NextSibling() != nil {
			out = append(out, "")
		}
	}
	r.listDepth--
	return out
}

func (r *Renderer) linesOf(n interface {
	Lines() *text.Segments
}) string {
	var sb strings.Builder
	segs := n.Lines()
	for i := 0; i < segs.Len(); i++ {
		seg := segs.At(i)
		sb.Write(seg.Value(r.src))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (r *Renderer) htmlBlock(h *ast.HTMLBlock, width int) []string {
	raw := r.linesOf(h)
	if h.HasClosure() {
		raw += "\n" + string(h.ClosureLine.Value(r.src))
	}
	raw = strings.TrimRight(raw, "\n")
	var out []string
	for _, line := range wrapHard([]span{{raw, attrs{html: true}}}, width) {
		out = append(out, r.renderLine(line))
	}
	return out
}
