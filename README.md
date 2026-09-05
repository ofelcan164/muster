# Muster

A [herdr](https://herdr.dev) plugin. One key opens a screen showing every agent
across every repo, ranked by what needs you, styled so you can tell them apart
before you read a word. Enter jumps you there. The same key brings you back.

Nothing here is implemented yet.

- `docs/plan.html` — the design. Open it in a browser. Source of truth until
  there is code.
- `docs/herdr-api-notes.md` — verified herdr 0.8.2 mechanics, including four
  things the docs get wrong or omit.

## Developing

```
mise install                 # go, pinned in .mise.toml
go build -o ./bin/muster  ./cmd/muster
go build -o ./bin/musterd ./cmd/musterd
herdr plugin link "$PWD"     # registers globally, undo with: herdr plugin unlink muster
```

`plugin link` does not run `[[build]]`, so build first.
