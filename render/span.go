package render

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// attrs describes how a run of text should be styled. It is a plain
// comparable value so adjacent spans can be merged and wrapped without ever
// touching ANSI escape sequences.
type attrs struct {
	bold, italic, strike, underline bool
	code                            bool // inline code
	codeBlock                       bool // inside a fenced/indented block (gets the block background)
	link                            bool
	url                             bool // a URL shown next to link text
	image                           bool
	html                            bool
	quote                           bool // inside a blockquote
	checked                         bool
	unchecked                       bool
	fg                              string // explicit foreground (syntax highlighting)
}

type span struct {
	text string
	a    attrs
}

// hardBreak is a sentinel span that forces a line break during wrapping.
var hardBreak = span{text: "\n"}

func (r *Renderer) styleFor(a attrs) lipgloss.Style {
	if s, ok := r.styleCache[a]; ok {
		return s
	}
	th := r.th
	s := lipgloss.NewStyle().Foreground(th.Text)
	if a.quote {
		s = s.Foreground(th.Subtle)
	}
	if a.codeBlock {
		s = s.Background(th.Surface0)
	}
	if a.fg != "" {
		s = s.Foreground(lipgloss.Color(a.fg))
	}
	switch {
	case a.code:
		s = s.Foreground(th.Code).Background(th.Surface1)
	case a.url:
		s = s.Foreground(th.Muted)
	case a.link:
		s = s.Foreground(th.Link).Underline(true)
	case a.image:
		s = s.Foreground(th.Image)
	case a.html:
		s = s.Foreground(th.Muted)
	case a.checked:
		s = s.Foreground(th.Checked).Bold(true)
	case a.unchecked:
		s = s.Foreground(th.Unchecked)
	}
	if a.bold {
		s = s.Bold(true)
	}
	if a.italic {
		s = s.Italic(true)
	}
	if a.strike {
		s = s.Strikethrough(true)
	}
	if a.underline {
		s = s.Underline(true)
	}
	r.styleCache[a] = s
	return s
}

// renderLine turns one wrapped line of spans into a styled string.
func (r *Renderer) renderLine(line []span) string {
	var sb strings.Builder
	for _, sp := range line {
		if sp.text == "" {
			continue
		}
		sb.WriteString(r.styleFor(sp.a).Render(sp.text))
	}
	return sb.String()
}

// renderLines wraps spans to width and styles every resulting line.
func (r *Renderer) renderLines(spans []span, width int) []string {
	lines := wrap(spans, width)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = r.renderLine(l)
	}
	return out
}

func lineWidth(line []span) int {
	w := 0
	for _, sp := range line {
		w += ansi.StringWidth(sp.text)
	}
	return w
}

// wrap performs greedy word wrapping over spans. Words never split across
// spans of different attrs unless a single word is wider than the line.
func wrap(spans []span, width int) [][]span {
	if width < 1 {
		width = 1
	}
	var (
		lines [][]span
		cur   []span
		curW  int
	)
	flush := func() {
		cur = trimRight(cur)
		lines = append(lines, cur)
		cur, curW = nil, 0
	}
	push := func(sp span) {
		if n := len(cur); n > 0 && cur[n-1].a == sp.a {
			cur[n-1].text += sp.text
		} else {
			cur = append(cur, sp)
		}
		curW += ansi.StringWidth(sp.text)
	}

	for _, sp := range spans {
		if sp == hardBreak {
			flush()
			continue
		}
		for _, piece := range splitWords(sp.text) {
			if piece == " " {
				if curW > 0 {
					push(span{" ", sp.a})
				}
				continue
			}
			pw := ansi.StringWidth(piece)
			if curW+pw > width && curW > 0 {
				flush()
			}
			if pw > width {
				for _, chunk := range chunkByWidth(piece, width) {
					if curW > 0 {
						flush()
					}
					push(span{chunk, sp.a})
				}
				continue
			}
			push(span{piece, sp.a})
		}
	}
	if len(cur) > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

// wrapHard breaks lines only at explicit newlines and at width, never at
// word boundaries. Used for code, where whitespace is significant.
func wrapHard(spans []span, width int) [][]span {
	if width < 1 {
		width = 1
	}
	var (
		lines [][]span
		cur   []span
		curW  int
	)
	flush := func() {
		lines = append(lines, cur)
		cur, curW = nil, 0
	}
	for _, sp := range spans {
		parts := strings.Split(sp.text, "\n")
		for i, part := range parts {
			if i > 0 {
				flush()
			}
			for part != "" {
				avail := width - curW
				if avail <= 0 {
					flush()
					continue
				}
				head, tail := takeWidth(part, avail)
				cur = append(cur, span{head, sp.a})
				curW += ansi.StringWidth(head)
				part = tail
			}
		}
	}
	if len(cur) > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

// splitWords splits text into words and single-space tokens, collapsing
// runs of whitespace (markdown treats them as one).
func splitWords(s string) []string {
	var out []string
	var word strings.Builder
	lastSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if word.Len() > 0 {
				out = append(out, word.String())
				word.Reset()
			}
			if !lastSpace {
				out = append(out, " ")
			}
			lastSpace = true
			continue
		}
		lastSpace = false
		word.WriteRune(r)
	}
	if word.Len() > 0 {
		out = append(out, word.String())
	}
	return out
}

// takeWidth splits s into a prefix of at most width cells and the rest.
func takeWidth(s string, width int) (head, tail string) {
	w := 0
	for i, r := range s {
		rw := ansi.StringWidth(string(r))
		if w+rw > width {
			return s[:i], s[i:]
		}
		w += rw
	}
	return s, ""
}

func chunkByWidth(s string, width int) []string {
	var out []string
	for s != "" {
		var head string
		head, s = takeWidth(s, width)
		if head == "" { // a single cell wider than width; emit it anyway
			_, size := firstRune(s)
			head, s = s[:size], s[size:]
		}
		out = append(out, head)
	}
	return out
}

func firstRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 0
}

func trimRight(line []span) []span {
	for len(line) > 0 {
		last := &line[len(line)-1]
		last.text = strings.TrimRight(last.text, " ")
		if last.text != "" {
			break
		}
		line = line[:len(line)-1]
	}
	return line
}
