//go:build unix

package graphics

import "testing"

func TestCapabilityRequiresPositiveHandshake(t *testing.T) {
	for _, tc := range []struct {
		s  string
		ok bool
	}{
		{"\x1bP>|kitty(0.48.2)\x1b\\\x1b_Gi=42;OK\x1b\\\x1b[6;22;11t", true},
		{"\x1bP>|kitty(0.27.0)\x1b\\\x1b_Gi=42;OK\x1b\\\x1b[6;22;11t", false},
		{"kitty(0.48.2)\x1b[6;22;11t", false},
		{"kitty(0.48.2);OK\x1b\\\x1b[6;0;0t", false},
		{"kitty(0.48.2);OK\x1b\\", false},
	} {
		c, ok := parseCapability(tc.s)
		if ok != tc.ok {
			t.Errorf("%q: %v", tc.s, ok)
		}
		if ok && (c.CellWidth != 11 || c.CellHeight != 22) {
			t.Fatal("invalid cell metrics")
		}
	}
}
func TestMultiplexerFallback(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TMUX", "/tmp/tmux")
	if Eligible() {
		t.Fatal("tmux enabled")
	}
	t.Setenv("TMUX", "")
	t.Setenv("STY", "screen")
	if Eligible() {
		t.Fatal("screen enabled")
	}
}
