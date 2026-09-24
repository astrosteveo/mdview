# Mermaid in Kitty

[Child document](mermaid-child.md) · [Nested diagram](#nested) · [Malformed fallback](#malformed)

## Flowchart

```mermaid
flowchart LR
    A[Markdown source] --> B{Kitty available?}
    B -->|Yes| C[Inline diagram]
    B -->|No| D[Original source]
    C --> E[Zoom · Pan · Copy 世界]
```

## Sequence

```mermaid
sequenceDiagram
    participant Reader
    participant Pager
    participant Mermaid
    Reader->>Pager: Open document
    Pager->>Mermaid: Render asynchronously
    Pager-->>Reader: Source stays visible
    Mermaid-->>Pager: PNG
    Pager-->>Reader: Inline diagram
```

## Class

```mermaid
classDiagram
    Document "1" *-- "many" Diagram
    class Document {
        +string source
        +reload()
    }
    class Diagram {
        +string source
        +zoom()
        +copySource()
    }
```

## ER

```mermaid
erDiagram
    DOCUMENT ||--o{ DIAGRAM : contains
    DOCUMENT {
        string path
    }
    DIAGRAM {
        string source
        string cacheKey
    }
```

## State

```mermaid
stateDiagram-v2
    [*] --> Source
    Source --> Rendering
    Rendering --> Diagram: success
    Rendering --> Source: error
    Diagram --> Viewer: enter
    Viewer --> Diagram: escape
```

## Gantt

```mermaid
gantt
    title Mermaid integration
    dateFormat YYYY-MM-DD
    section Delivery
    Build :a, 2026-09-24, 2d
    Verify :after a, 2d
    Install :after a, 1d
```

## Nested

> A diagram in a quoted list:
>
> - ```mermaid
>   flowchart TD
>       A[Quoted list] --> B[Indentation retained]
>       B --> C[Scrolls with the document]
>   ```

## Repeated

```mermaid
flowchart TD
    A[Quoted list] --> B[Indentation retained]
    B --> C[Scrolls with the document]
```

## Malformed

This block intentionally stays as source without a notification.

```mermaid
flowchart LR
    A --> [
```

## End

[Back to top](#mermaid-in-kitty)
