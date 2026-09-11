package render

import (
	"regexp"
	"strconv"
	"strings"
)

// Line is one rendered row: styled text plus the clickable ranges in it.
type Line struct {
	Text   string
	Links  []Link
	Anchor string // heading slug, set on a heading's first line
}

// Link is a clickable cell range [Start, End) within a Line.
type Link struct {
	Start, End int
	Dest       string
}

// Doc is a rendered document.
type Doc struct {
	Lines []Line
	// Anchors maps GitHub-style heading slugs to line indexes.
	Anchors map[string]int
}

// Text joins the rendered lines.
func (d Doc) Text() string {
	parts := make([]string, len(d.Lines))
	for i, l := range d.Lines {
		parts[i] = l.Text
	}
	return strings.Join(parts, "\n")
}

// LinkAt returns the link covering cell col on line n, if any.
func (d Doc) LinkAt(n, col int) (Link, bool) {
	if n < 0 || n >= len(d.Lines) {
		return Link{}, false
	}
	for _, l := range d.Lines[n].Links {
		if col >= l.Start && col < l.End {
			return l, true
		}
	}
	return Link{}, false
}

func (l *Line) addLink(start, end int, dest string) {
	if n := len(l.Links); n > 0 && l.Links[n-1].End == start && l.Links[n-1].Dest == dest {
		l.Links[n-1].End = end
		return
	}
	l.Links = append(l.Links, Link{start, end, dest})
}

// shift moves every link right by w cells (a prefix was added).
func (l Line) shift(w int) Line {
	if w == 0 || len(l.Links) == 0 {
		return l
	}
	links := make([]Link, len(l.Links))
	for i, lk := range l.Links {
		links[i] = Link{lk.Start + w, lk.End + w, lk.Dest}
	}
	l.Links = links
	return l
}

// prefix prepends p (of cell width w) to the line text.
func (l Line) prefix(p string, w int) Line {
	l = l.shift(w)
	l.Text = p + l.Text
	return l
}

// scaleCols multiplies link columns for kitty-scaled headings.
func (l Line) scaleCols(s int) Line {
	if s <= 1 {
		return l
	}
	for i, lk := range l.Links {
		l.Links[i] = Link{lk.Start * s, lk.End * s, lk.Dest}
	}
	return l
}

var slugStrip = regexp.MustCompile(`[^\p{L}\p{N}\s-]`)

// Slug produces a GitHub-style heading anchor: lowercase, punctuation
// removed, spaces to hyphens.
func Slug(heading string) string {
	s := strings.ToLower(strings.TrimSpace(heading))
	s = slugStrip.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

// collectAnchors indexes heading slugs, suffixing duplicates like GitHub.
func collectAnchors(lines []Line) map[string]int {
	anchors := map[string]int{}
	for i, l := range lines {
		if l.Anchor == "" {
			continue
		}
		slug := l.Anchor
		for n := 1; ; n++ {
			if _, taken := anchors[slug]; !taken {
				break
			}
			slug = l.Anchor + "-" + strconv.Itoa(n)
		}
		anchors[slug] = i
	}
	return anchors
}
