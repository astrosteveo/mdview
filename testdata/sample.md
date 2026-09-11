# mdview

A tiny, opinionated **Markdown viewer** for the terminal. It parses with
*goldmark*, styles with `lipgloss`, and highlights code with chroma. Visit
[the repo](https://github.com/example/mdview) or <https://example.com>.

## Lists & tasks

- [x] Parse CommonMark + GFM
- [ ] Footnotes
- [ ] A task with a long description that has to wrap around onto the next line to prove hanging indents work
- Plain bullet with ~~strikethrough~~ and a nested list:
  - Nested item one
  - Nested item two
    1. Ordered inside
    2. Still ordered

1. First
2. Second
10. Tenth, to check marker alignment

> A blockquote. It can hold **bold** text and wraps cleanly when the line
> gets long enough to need it, which this one certainly does.
>
> > Nested quote with `code`.

### Code

```go
package main

import "fmt"

func main() {
	fmt.Println("hello, mdview") // greeting
}
```

```
no language here
```

#### Table

| Name    | Role         |  Score |
|:--------|:------------:|-------:|
| Ada     | Engineer     |     98 |
| Grace   | Rear Admiral |    100 |
| Linus   | Kernel       |     93 |

##### Image and HTML

![A diagram](diagram.png) then some <em>raw html</em> inline.

<div align="center">block html</div>

---

###### The end
