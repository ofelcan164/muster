# Muster

A herdr plugin: one overlay showing every agent across every repo. Go, bubbletea,
lipgloss. A daemon (`musterd`) writes a snapshot; the overlay (`muster`) reads it.

## Build and test

```
go build -o ./bin/muster ./cmd/muster && go build -o ./bin/musterd ./cmd/musterd
go test ./... -race
```

The manifest builds both binaries into `bin/`. Rebuild them after changing the
overlay, or the live session keeps running the old one.

## Rules that are not obvious from the code

- **Events schedule a reconcile, nothing more.** herdr replays the whole session
  history on every subscribe, out of causal order, including dead panes. State is
  always rebuilt from `session.snapshot`. Folding events into state looks fine in
  testing and is wrong.
- **`pane.read` costs 350ms, `pane.process_info` costs 0.8ms.** Never put a
  `pane.read` on the reconcile path.
- **The daemon owns `state.json`** and rewrites it several times a second. The
  overlay writes `ui.json`. Do not merge them.
- **herdr mouse coordinates are pane-local and 0-based.** Measured against a
  real mouse, not assumed. No offset correction belongs anywhere.
- **`AgeKnown` false means a lower bound.** Fine for a threshold, wrong for
  ordering. `internal/triage` handles both cases explicitly; do not collapse them.
- **Nothing is hardcoded about any repo.** Names, colours, sigils and grid slots
  are derived at runtime.

## Working here

- **Use edits that fail loudly.** Three bugs came from scripted string replacement
  that silently matched nothing after gofmt realigned the source. Assert on every
  replacement.
- **Stage explicit paths.** A broad `git add` has twice swept unrelated work into
  a commit.
- **Do not launch the herdr TUI from an agent session.** It hangs. Drive the
  server from the CLI and read the overlay with `herdr pane read <pane>`.
- **Ask before prompting the user's agents.** They cost the user tokens.
- `herdr pane send-keys` silently ignores an invalid key name. `ctrl+c` works,
  `ctrl-c` does nothing.
- lipgloss strips styling when it cannot detect a colour terminal, which a test
  binary never has. `internal/ui/ui_test.go` forces the profile in `init`.
  Without it, tests asserting on styling measure nothing and pass.
