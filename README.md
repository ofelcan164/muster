# Muster

A [herdr](https://herdr.dev) plugin. One key opens a screen showing every agent
across every repo, ranked by what needs you, styled so you can tell them apart
before you read a word. Enter jumps you there. The same key brings you back.

- `docs/plan.html` — the design. Open it in a browser.
- `docs/herdr-api-notes.md` — verified herdr 0.8.2 mechanics, including the
  event subscription behaviour the daemon is built on.

## What works so far

`musterd`, the daemon. The overlay is not built yet, because the overlay is
worthless until something is watching.

The daemon holds one event subscription on `$HERDR_SOCKET_PATH`, discovers repos
from workspace cwds, ranks agents by the triage table, and keeps a snapshot at
`$HERDR_PLUGIN_STATE_DIR/snapshot.json`. Reading and decoding that snapshot
takes 17µs on average and 167µs at worst, against a 5ms budget.

```
musterd --ensure       start a daemon if one is not running, then exit
musterd dump [--json]  print the snapshot
musterd status         is a daemon running, and how fresh is the snapshot
```

`musterd dump` is the proving ground. No TUI until that output is right.

## Developing

```
mise install
mise exec -- go build -o ./bin/muster  ./cmd/muster
mise exec -- go build -o ./bin/musterd ./cmd/musterd
herdr plugin link "$PWD"     # undo with: herdr plugin unlink muster
mise exec -- go test ./...
```

`plugin link` does not run `[[build]]`, so build first. Manifest commands
resolve through `PATH` rather than the plugin root, so every one of them needs
the leading `./`.

Testing needs a running herdr server with a few agents. Launching the TUI from
an agent session will hang it; drive the server from the CLI instead.

## How the daemon is put together

- `internal/herdr` — socket client. RPC and the long-lived subscription. Never
  shells out to the herdr binary.
- `internal/discover` — workspace cwd to repo identity. Reads `.git` directly
  rather than forking git.
- `internal/identity` — colour, sigil and border from the repo key.
- `internal/triage` — the attention ranking.
- `internal/daemon` — reconcile loop, lifecycle, `dump`.
- `internal/state` — state dir, lock, atomic snapshot writes.

Three design points worth knowing before changing any of it.

**Events are a hint, never a log.** herdr replays the whole session event
history on every subscribe, at ten events a second, including events for panes
that no longer exist and in an order that is not causal. Every event does one
thing: schedule a reconcile. State is always rebuilt from `session.snapshot`.

**The daemon subscribes only to globally-scoped event types.** Per-pane types
need a `pane_id`, and a second `events.subscribe` on a live connection makes the
server reset it, so a per-pane subscription could never be extended to a new
pane. `pane.updated` is global and carries the whole pane record, which covers
the same ground.

**Nothing is hardcoded about any repo.** Names, colours, sigils and grid slots
are all derived at runtime and stored in Muster's own state dir. Grid slots are
handed out in workspace-number order rather than map order, so a fresh install
lays out the same way twice.
