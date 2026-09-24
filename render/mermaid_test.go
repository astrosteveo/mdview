package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMermaidMetadataNestedRepeatedAndFallback(t *testing.T) {
	src := []byte("# Before\n\n```mermaid\nflowchart LR\n A --> B\n```\n\n> - ```mermaid\n>   flowchart LR\n>    A --> B\n>   ```\n\n```go\nmermaid := 1\n```\n\n    mermaid\n\n# After\n")
	r := New(Mocha())
	doc := r.RenderDoc(src, 60)
	blocks := map[int]*MermaidBlock{}
	indents := map[int]int{}
	for _, line := range doc.Lines {
		if b := line.Mermaid; b != nil {
			blocks[b.ID] = b
			indents[b.ID] = line.Indent
			if !strings.Contains(b.Source, "A --> B") {
				t.Fatalf("lost source %q", b.Source)
			}
		}
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks=%v", blocks)
	}
	if blocks[1] == blocks[2] || blocks[1].Source != blocks[2].Source {
		t.Fatal("repeated diagrams need distinct identity and identical source")
	}
	if indents[1] != 0 || indents[2] != 4 || blocks[2].Width != 56 {
		t.Fatalf("nested geometry: %v, %+v", indents, blocks[2])
	}
	if !strings.Contains(ansi.Strip(doc.Text()), "A --> B") || strings.Contains(doc.Text(), "\U0010eeee") {
		t.Fatal("ordinary rendering must retain code")
	}
	if doc.Anchors["after"] <= doc.Anchors["before"] {
		t.Fatal("lost heading anchors")
	}
	resized := r.RenderDoc(src, 30)
	for _, line := range resized.Lines {
		if b := line.Mermaid; b != nil && b.Source != blocks[b.ID].Source {
			t.Fatal("resize changed source or identity")
		}
	}
}
