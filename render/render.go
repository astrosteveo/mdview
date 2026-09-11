// Package render turns Markdown into styled terminal text.
//
// The pipeline is: goldmark parses to an AST; block nodes are rendered to
// lines recursively (each container narrows the width and prefixes its
// children); inline nodes become styled spans that are word-wrapped before
// any ANSI is emitted, so styles survive line breaks and indentation.
package render

import (
	"fmt"
	"regexp"
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
	// BigHeadings renders H1–H3 with kitty's text sizing protocol (OSC 66).
	// Only enable on terminals that support it; see Unscale for the fallback.
	BigHeadings bool
	// OSC8 wraps absolute URLs in terminal hyperlinks (OSC 8).
	OSC8 bool

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
	return r.RenderDoc(src, width).Text()
}

// RenderDoc renders markdown source to lines with link and anchor metadata.
func (r *Renderer) RenderDoc(src []byte, width int) Doc {
	if width < 20 {
		width = 20
	}
	r.src = src
	r.quoteDepth, r.listDepth, r.skipCheckbox = 0, 0, false
	root := md.Parser().Parse(text.NewReader(src))
	lines := r.blocks(root, width, false)
	return Doc{Lines: lines, Anchors: collectAnchors(lines)}
}

// blocks renders each child of parent, separating them with a blank line
// unless the container is a tight list item.
func (r *Renderer) blocks(parent ast.Node, width int, tight bool) []Line {
	var out []Line
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		lines := r.block(c, width, tight)
		if len(lines) == 0 {
			continue
		}
		if len(out) > 0 && !tight {
			out = append(out, Line{})
		}
		out = append(out, lines...)
	}
	return out
}

func (r *Renderer) block(n ast.Node, width int, tight bool) []Line {
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
		return []Line{r.plainLine(r.rule("─", width, r.th.Rule))}
	case *east.Table:
		return r.table(v, width)
	default:
		return r.blocks(v, width, tight)
	}
}

func (r *Renderer) heading(h *ast.Heading, width int) []Line {
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
	sc := headingScales[level-1]
	if !r.BigHeadings {
		sc = scale{s: 1}
	}
	spans := r.inlines(h)
	// Headings are rendered as plain runs so the heading colour wins, but
	// inline code inside them keeps its chip.
	var out []Line
	for _, line := range wrap(spans, max(width/sc.s, 1)) {
		var sb strings.Builder
		var ln Line
		col := 0
		for _, sp := range line {
			sty := base
			if sp.a.code {
				sty = r.styleFor(sp.a)
			}
			w := ansi.StringWidth(sp.text)
			if sp.a.href != "" {
				ln.addLink(col, col+w, sp.a.href)
			}
			sb.WriteString(sc.render(sty, sp.text))
			col += w
		}
		ln.Text = sb.String()
		ln = ln.scaleCols(sc.s)
		if len(out) == 0 {
			ln.Anchor = Slug(plainText(spans))
		}
		out = append(out, ln)
		// Rows below a scaled block belong to it; skip past the block and
		// clear the rest of the row rather than writing over it.
		for i := 1; i < sc.s; i++ {
			out = append(out, r.plainLine(fmt.Sprintf("\x1b[%dC\x1b[K", lineWidth(line)*sc.s)))
		}
	}
	switch level {
	case 1:
		out = append(out, r.plainLine(r.rule("━", width, color)))
	case 2:
		out = append(out, r.plainLine(r.rule("─", width, r.th.Rule)))
	}
	return out
}

// scale is a kitty text-sizing spec: the glyphs occupy an s×s cell block per
// character, optionally shrunk to n/d of that and aligned vertically by v.
type scale struct{ s, n, d, v int }

var headingScales = [6]scale{
	{s: 2},                   // H1: 2x
	{s: 2, n: 3, d: 4, v: 1}, // H2: 1.5x, bottom-aligned
	{s: 2, n: 5, d: 8, v: 1}, // H3: 1.25x
	{s: 1}, {s: 1}, {s: 1},
}

// render styles text and, for scaled headings, wraps it in OSC 66 so the
// active SGR attributes apply to the enlarged glyphs.
func (sc scale) render(sty lipgloss.Style, text string) string {
	rendered := sty.Render(text)
	if sc.s <= 1 || text == "" {
		return rendered
	}
	meta := fmt.Sprintf("s=%d", sc.s)
	if sc.d > 0 {
		meta += fmt.Sprintf(":n=%d:d=%d:v=%d", sc.n, sc.d, sc.v)
	}
	// lipgloss emits <SGR...>text<reset>; keep the SGR prefix, swap the text.
	i := 0
	for strings.HasPrefix(rendered[i:], "\x1b[") {
		j := strings.IndexByte(rendered[i:], 'm')
		if j < 0 {
			break
		}
		i += j + 1
	}
	if !strings.HasPrefix(rendered[i:], text) {
		return rendered
	}
	return rendered[:i] + "\x1b]66;" + meta + ";" + text + "\x1b\\" + rendered[i+len(text):]
}

var osc66 = regexp.MustCompile(`\x1b\]66;[^;\x1b]*;([^\x1b]*)\x1b\\`)

// Unscale strips kitty text-sizing wrappers, leaving the styled 1x text.
func Unscale(line string) string {
	return osc66.ReplaceAllString(line, "$1")
}

func (r *Renderer) rule(ch string, width int, color lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(color).Render(strings.Repeat(ch, width))
}

func (r *Renderer) blockquote(q *ast.Blockquote, width int) []Line {
	r.quoteDepth++
	inner := r.blocks(q, width-2, false)
	r.quoteDepth--
	bar := lipgloss.NewStyle().Foreground(r.th.Quote).Render("┃")
	for i, l := range inner {
		if l.Text == "" {
			inner[i] = r.plainLine(bar)
		} else {
			inner[i] = l.prefix(bar+" ", 2)
		}
	}
	return inner
}

var bullets = []string{"•", "◦", "▪", "▫"}

func (r *Renderer) list(l *ast.List, width int) []Line {
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

	var out []Line
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
			lines = []Line{{}}
		}
		pad := strings.Repeat(" ", hang)
		for i, ln := range lines {
			if i == 0 {
				out = append(out, ln.prefix(marker, hang))
			} else {
				out = append(out, ln.prefix(pad, hang))
			}
		}
		if !l.IsTight && item.NextSibling() != nil {
			out = append(out, Line{})
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

func (r *Renderer) htmlBlock(h *ast.HTMLBlock, width int) []Line {
	raw := r.linesOf(h)
	if h.HasClosure() {
		raw += "\n" + string(h.ClosureLine.Value(r.src))
	}
	raw = strings.TrimRight(raw, "\n")
	var out []Line
	for _, line := range wrapHard([]span{{raw, attrs{html: true}}}, width) {
		out = append(out, r.renderLine(line))
	}
	return out
}
