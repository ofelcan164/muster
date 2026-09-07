# Muster

A herdr plugin. One overlay, one keystroke, showing every agent across every
repo you have work in: what each is doing, which ones need you, and which
finished without anyone noticing.

The last of those is the point. The failure it was built for is an agent that
finishes, an orchestrator that never learns, and three downstream agents idling
against a gate that already opened. Muster detects that and gives you one key to
repair it.

## Shape

Two binaries. `musterd` is a background daemon that polls herdr and writes
`snapshot.json` into the plugin state dir. `muster` is a short-lived client:
every keypress spawns one, it reads the snapshot, draws, and exits. The daemon
exists because opening this screen a hundred times a day has to be free, and
because something has to watch while the overlay is closed.

```
cmd/muster            client: the overlay, and every action the manifest binds
cmd/musterd           daemon: reconcile loop, dump, ensure

internal/herdr        socket client. Never shells out to the herdr binary
internal/daemon       reconcile loop, lifecycle, snapshot writing
internal/discover     workspace cwd to repo identity. Reads .git directly
internal/identity     colour, sigil and border, derived from the repo key
internal/chain        the orchestrator-recorded dependency order
internal/triage       the attention ranking that fills the ribbon
internal/state        state dir, lock, atomic writes, everything persisted
internal/install      the only code that writes files the user owns
internal/model        the snapshot types both sides share
internal/ui           the overlay
```

`internal/ui` splits by concern: `model` (lifecycle and dispatch), `targets`
(what the selection can land on), `keys`, `mouse`, `filter`, `order`, `strip`
(the orchestrator strip), `talk` (the `i` and `t` keys), `view`, `theme`.

## Build and test

```
go build -o ./bin/muster ./cmd/muster && go build -o ./bin/musterd ./cmd/musterd
go test ./... -race
```

The manifest builds both into `bin/`. Rebuild after touching the overlay or the
live session keeps running the old one.

## Commands

```
muster open | jump orchestrator | jump previous     what the keybindings invoke
muster install | uninstall [--purge]                keybindings, and undoing them
muster install-skill | uninstall-skill              the orchestrator reporting skill
muster mark-orchestrator                            run on the orchestrator's pane
muster chain get | set <spec> | clear               "contracts > api > web,mobile"

musterd --ensure          start a daemon if none is running, then exit at once
musterd --daemon          run as the daemon
musterd dump [--json]     the supported way to read the snapshot
musterd status
```

Global keys, once installed: `prefix+m` toggles the overlay, `prefix+shift+m`
jumps to the orchestrator, `prefix+ctrl+m` goes back.

In the overlay: arrows or `hjkl` move, `enter` jumps, `/` searches, `s` cycles
sort, `J`/`K` rearrange, `g`/`G` jump to the ends, digits jump to a ribbon row,
`i` messages the orchestrator, `t` reports a finished agent to it, `q` closes.

## Driving it while you work

Do not launch the herdr TUI from an agent session, it hangs. Drive the server
from the CLI instead and read the overlay's own pane:

```
herdr pane list
herdr pane read <pane-id>
musterd dump
```

Ask before prompting the user's agents. They cost the user tokens.
