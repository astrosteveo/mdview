package render

import (
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

// inlines flattens the inline children of n into styled spans.
func (r *Renderer) inlines(n ast.Node) []span {
	base := attrs{quote: r.quoteDepth > 0}
	var out []span
	r.walkInline(n, base, &out)
	return out
}

func (r *Renderer) walkInline(n ast.Node, cur attrs, out *[]span) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *ast.Text:
			*out = append(*out, span{string(v.Segment.Value(r.src)), cur})
			if v.HardLineBreak() {
				*out = append(*out, hardBreak)
			} else if v.SoftLineBreak() {
				*out = append(*out, span{" ", cur})
			}
		case *ast.String:
			*out = append(*out, span{string(v.Value), cur})
		case *ast.Emphasis:
			a := cur
			if v.Level >= 2 {
				a.bold = true
			} else {
				a.italic = true
			}
			r.walkInline(v, a, out)
		case *east.Strikethrough:
			a := cur
			a.strike = true
			r.walkInline(v, a, out)
		case *ast.CodeSpan:
			a := cur
			a.code = true
			r.walkInline(v, a, out)
		case *ast.Link:
			a := cur
			a.link = true
			a.href = string(v.Destination)
			start := len(*out)
			r.walkInline(v, a, out)
			dest := string(v.Destination)
			if !r.NoURLs && dest != "" && dest != plainText((*out)[start:]) {
				u := cur
				u.url = true
				*out = append(*out, span{" (" + dest + ")", u})
			}
		case *ast.AutoLink:
			a := cur
			a.link = true
			a.href = string(v.URL(r.src))
			*out = append(*out, span{string(v.URL(r.src)), a})
		case *ast.Image:
			a := cur
			a.image = true
			a.href = string(v.Destination)
			alt := plainTextOf(v, r.src)
			if alt == "" {
				alt = "image"
			}
			*out = append(*out, span{"▨ " + alt, a})
			if !r.NoURLs && len(v.Destination) > 0 {
				u := cur
				u.url = true
				*out = append(*out, span{" (" + string(v.Destination) + ")", u})
			}
		case *ast.RawHTML:
			a := cur
			a.html = true
			for i := 0; i < v.Segments.Len(); i++ {
				seg := v.Segments.At(i)
				*out = append(*out, span{string(seg.Value(r.src)), a})
			}
		case *east.TaskCheckBox:
			if r.skipCheckbox {
				r.skipCheckbox = false
				continue
			}
			*out = append(*out, checkboxSpan(v.IsChecked))
		default:
			r.walkInline(v, cur, out)
		}
	}
}

func checkboxSpan(checked bool) span {
	if checked {
		return span{"[✓] ", attrs{checked: true}}
	}
	return span{"[ ] ", attrs{unchecked: true}}
}

func plainText(spans []span) string {
	s := ""
	for _, sp := range spans {
		s += sp.text
	}
	return s
}

// plainTextOf returns the unstyled text content of an inline container.
func plainTextOf(n ast.Node, src []byte) string {
	s := ""
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *ast.Text:
			s += string(v.Segment.Value(src))
		case *ast.String:
			s += string(v.Value)
		default:
			s += plainTextOf(v, src)
		}
	}
	return s
}

// startsWithCheckbox reports whether a list item's first paragraph begins
// with a GFM task checkbox, so the bullet can be replaced by it.
func startsWithCheckbox(item ast.Node) (checked, ok bool) {
	first := item.FirstChild()
	if first == nil {
		return false, false
	}
	if _, isPara := first.(*ast.Paragraph); !isPara {
		if _, isText := first.(*ast.TextBlock); !isText {
			return false, false
		}
	}
	cb, isCB := first.FirstChild().(*east.TaskCheckBox)
	if !isCB {
		return false, false
	}
	return cb.IsChecked, true
}
