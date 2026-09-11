# mdview

Your own damn Markdown viewer. A single Go binary that renders Markdown in
the terminal — an interactive pager when attached to a TTY, plain styled
output when piped.

```
mdview README.md            # interactive pager
mdview -theme latte doc.md  # light palette
cat notes.md | mdview       # stdin (still gets the pager)
mdview -pager never doc.md  # print and exit
mdview -color always x.md | less -R
```

## What it renders

Headings (with rules), paragraphs with **bold**/*italic*/~~strike~~/`code`,
links (text + dim URL), images, bullet/ordered/task lists with hanging
indents, nested blockquotes, syntax-highlighted fenced code (Chroma, ~250
languages), GFM tables with alignment, thematic breaks, raw HTML (dimmed).

## Keys

| Key            | Action                |
|----------------|-----------------------|
| `j`/`k`, arrows, wheel | scroll        |
| `d`/`u`        | half page             |
| `space`/`f`, `b` | full page           |
| `g`/`G`        | top / bottom          |
| `/` … `enter`  | search (case-insensitive) |
| `n`/`N`        | next / previous match |
| `r`            | reload file           |
| `q`, `esc`     | quit                  |

The file is polled every 500 ms and re-rendered on change, so it works as a
live preview beside your editor.

## Flags

```
-w N           content width (0 = terminal width, capped at -max)
-max N         max content width in the pager (default 110; 0 = unlimited)
-theme NAME    mocha|dark (default) or latte|light
-pager MODE    auto|always|never
-color MODE    auto|always|never
-no-urls       hide link/image destinations
```

## Build

```
go build -o mdview .
go test ./...
```

## How it works

`render/` parses with [goldmark](https://github.com/yuin/goldmark) (GFM
extensions on) and walks the AST itself. Inline content becomes a list of
`(text, attrs)` spans that are word-wrapped *before* any ANSI is emitted, so
styles survive line breaks and container indentation. Containers (lists,
quotes) render their children at a narrower width and prefix each line.
Code blocks go through Chroma's tokenizer and get per-token colours from the
theme's Chroma style. Tables use `lipgloss/table` for column sizing.

`tui/` is a [Bubble Tea](https://github.com/charmbracelet/bubbletea)
viewport with a status bar, search over ANSI-stripped lines, and an mtime
poll for live reload. Themes live in `render/theme.go`.
