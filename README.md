# Muster

A [herdr](https://herdr.dev) plugin. One overlay, one keystroke, showing every
agent across every repo you have work in: what each is doing, which ones need
you, and which finished without anyone noticing.

That last one is why it exists. An agent finishes, the orchestrator never
learns, and three downstream agents idle against a gate that already opened.
Muster shows you that and gives you one key to repair it.

- an **overlay** (`prefix+m`) — every agent, grouped by repo, ranked by what
  needs you, each repo with its own colour, sigil and border
- **jump to the orchestrator** (`prefix+shift+m`) and **back** (`prefix+ctrl+m`)
- **`i` and `t`** — message the orchestrator, or report a finished agent to it,
  without leaving the overlay
- a **chain** — the dependency order the orchestrator recorded
  (`contracts > api > web,mobile`), so a finished agent shows who it unblocks

## Install

Not published yet, so link it locally:

```sh
git clone https://github.com/ofelcan164/muster && cd muster
mise exec -- go build -o ./bin/muster ./cmd/muster
mise exec -- go build -o ./bin/musterd ./cmd/musterd
herdr plugin link "$PWD"
```

Then run the **Install Muster's keybindings** action, or `./bin/muster install`.
`muster uninstall` puts your config back.

Optionally, **Install the reporting skill for the orchestrator** teaches your
orchestrator agent to record the chain and report completions. That writes to
your Claude directory, so it stays a separate opt-in.

## Keys

In the overlay: arrows or `hjkl` move, `enter` jumps, `/` searches, `s` cycles
sort, `J`/`K` rearrange, `g`/`G` jump to the ends, digits jump to a ribbon row,
`x` dismisses one, `o` marks the selected agent as the orchestrator, `q` closes.

## How it works

Two binaries. `musterd` is a daemon holding one event subscription on the herdr
socket; it rebuilds state from `session.snapshot` and writes `snapshot.json`
into the plugin state dir. `muster` is short-lived: every keypress spawns one,
it reads the snapshot, draws, and exits.

The daemon exists because opening this screen a hundred times a day has to be
free, and because something has to watch while the overlay is closed. Reading
and decoding the snapshot takes 17µs on average, 167µs at worst.

Nothing is hardcoded about any repo. Names, colours, sigils and grid slots are
derived at runtime, so a fresh install lays out the same way twice.

```sh
musterd dump [--json]    print the snapshot
musterd status           is a daemon running, and how fresh is the snapshot
muster chain set "contracts > api > web,mobile"
```

## Developing

```sh
mise install
mise exec -- go build -o ./bin/muster ./cmd/muster
mise exec -- go build -o ./bin/musterd ./cmd/musterd
mise exec -- go test ./... -race
herdr plugin link "$PWD"     # undo with: herdr plugin unlink muster
```

`plugin link` does not run `[[build]]`, so build first. Manifest commands
resolve through `PATH` rather than the plugin root, so every one needs the
leading `./`. Testing needs a running herdr server with a few agents; launching
the herdr TUI from an agent session will hang it, so drive it from the CLI.

`docs/herdr-api-notes.md` has the verified herdr 0.8.2 mechanics the daemon is
built on, including why events are a hint and never a log.

## License

MIT. See [LICENSE](LICENSE).
