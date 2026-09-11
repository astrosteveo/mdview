package render

import "github.com/charmbracelet/lipgloss"

// Theme holds every colour the renderer uses. Palettes are Catppuccin
// (MIT-licensed), which pairs well with a matching Chroma style.
type Theme struct {
	Name string

	Text     lipgloss.Color // body copy
	Subtle   lipgloss.Color // blockquote text, secondary
	Muted    lipgloss.Color // URLs, raw HTML, hints
	Surface0 lipgloss.Color // code block background
	Surface1 lipgloss.Color // inline code / chip background
	Mantle   lipgloss.Color // status bar background

	Heading [6]lipgloss.Color

	Code      lipgloss.Color // inline code foreground
	CodeLabel lipgloss.Color // language chip foreground
	Link      lipgloss.Color
	Bullet    lipgloss.Color
	Number    lipgloss.Color
	Quote     lipgloss.Color
	Rule      lipgloss.Color
	Checked   lipgloss.Color
	Unchecked lipgloss.Color
	Image     lipgloss.Color
	Table     lipgloss.Color
	Accent    lipgloss.Color

	ChromaStyle string
}

func Mocha() Theme {
	return Theme{
		Name:     "mocha",
		Text:     "#cdd6f4",
		Subtle:   "#bac2de",
		Muted:    "#7f849c",
		Surface0: "#313244",
		Surface1: "#45475a",
		Mantle:   "#181825",
		Heading: [6]lipgloss.Color{
			"#cba6f7", // mauve
			"#fab387", // peach
			"#f9e2af", // yellow
			"#a6e3a1", // green
			"#94e2d5", // teal
			"#89dceb", // sky
		},
		Code:        "#a6e3a1",
		CodeLabel:   "#89b4fa",
		Link:        "#89b4fa",
		Bullet:      "#89dceb",
		Number:      "#89dceb",
		Quote:       "#f5c2e7",
		Rule:        "#585b70",
		Checked:     "#a6e3a1",
		Unchecked:   "#7f849c",
		Image:       "#f5c2e7",
		Table:       "#89b4fa",
		Accent:      "#cba6f7",
		ChromaStyle: "catppuccin-mocha",
	}
}

func Latte() Theme {
	return Theme{
		Name:     "latte",
		Text:     "#4c4f69",
		Subtle:   "#5c5f77",
		Muted:    "#8c8fa1",
		Surface0: "#e6e9ef",
		Surface1: "#dce0e8",
		Mantle:   "#e6e9ef",
		Heading: [6]lipgloss.Color{
			"#8839ef", // mauve
			"#fe640b", // peach
			"#df8e1d", // yellow
			"#40a02b", // green
			"#179299", // teal
			"#04a5e5", // sky
		},
		Code:        "#40a02b",
		CodeLabel:   "#1e66f5",
		Link:        "#1e66f5",
		Bullet:      "#04a5e5",
		Number:      "#04a5e5",
		Quote:       "#ea76cb",
		Rule:        "#acb0be",
		Checked:     "#40a02b",
		Unchecked:   "#8c8fa1",
		Image:       "#ea76cb",
		Table:       "#1e66f5",
		Accent:      "#8839ef",
		ChromaStyle: "catppuccin-latte",
	}
}

// ByName resolves a theme by name; "dark"/"light" are aliases.
func ByName(name string) (Theme, bool) {
	switch name {
	case "", "mocha", "dark":
		return Mocha(), true
	case "latte", "light":
		return Latte(), true
	}
	return Theme{}, false
}
