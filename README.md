# Muster

A [herdr](https://herdr.dev) plugin. One overlay, one keystroke, showing every
agent across every repo you have work in: what each is doing, which ones need
you, and which finished without anyone noticing.

That last one is why it exists. An agent finishes, the orchestrator never
learns, and three downstream agents idle against a gate that already opened.
Muster shows you that and gives you one key to repair it.

![The Muster overlay: a ribbon of the agents that need you, above one card per
repo showing each agent's status, age and task line, with the orchestrator's
last message along the bottom](docs/overlay.png)

## Install

```sh
herdr plugin install ofelcan164/muster
```

That is the whole install. Muster binds its keys itself: the startup hook runs
`muster install --auto`, which starts the daemon and writes the keybindings.
Installing mid-session gets no startup hook, so opening the overlay once from
herdr's action menu does the same job.

To keep the overlay and skip the global keys, run `muster install --no-keys`,
or `muster uninstall-keys` if they are already in. Either records the refusal
in the state dir, and `--auto` honours it from then on, so the startup hook
stops putting them back. The **Install Muster's keybindings** action asks for
them again. (The **Uninstall Muster's keybindings and skill** action is the
full removal, skill included, not a way to drop just the keys.)

From source instead, if you want to hack on it:

```sh
git clone https://github.com/ofelcan164/muster && cd muster
go build -o ./bin/muster ./cmd/muster
go build -o ./bin/musterd ./cmd/musterd
herdr plugin link "$PWD"
```

`plugin link` does not run `[[build]]`, so build first. Manifest commands
resolve through `PATH` rather than the plugin root, so every one needs the
leading `./`. In a source checkout you can also run `./bin/muster install`
directly from a herdr pane; with `plugin install` the binaries live in herdr's
own checkout, so use the action.

Install writes: a marked block in your herdr config (with a timestamped backup
beside it), a detached daemon, that daemon's `musterd.log` and lock files in
the plugin state dir, and a `server.reload_config` call so the keys go live.
The daemon adds `snapshot.json` and `state.json` once it reaches herdr.
`ui.json` appears when you first close the overlay and `chain.json` when you
first set a chain.

The reporting skill is the exception. **Install the reporting skill for the
orchestrator** writes to your Claude directory, so it stays a separate opt-in.

Commands that touch state (`install`, `chain`, `musterd dump`) run inside a
herdr pane, where `HERDR_PLUGIN_STATE_DIR` is set. From a plain shell they fail
with "no state directory": pass `--state-dir <dir>`.

## Requirements

- herdr 0.8.2 or newer (tested against 0.8.2 and 0.9.0)
- Go on `PATH`, any version from 1.21. `go.mod` asks for 1.24 and the default
  `GOTOOLCHAIN=auto` fetches it, so an older Go still builds this. A Go with
  `GOTOOLCHAIN=local` set, which some distro packages do, needs 1.24 itself
- Linux or macOS
- Claude Code, only for the reporting skill

## Keys

`prefix` is your own herdr prefix key, from your herdr config.

Global, once installed: `prefix+m` opens Muster, `prefix+shift+m` jumps
straight to the orchestrator, `prefix+ctrl+m` goes back to the previous agent.

If you have already bound `prefix+m` yourself, yours wins and Muster takes the
next free letter, saying which one. Name your own with
`muster install --key <letter>`; that choice sticks across restarts. Muster
never installs without a key, because the overlay would then only be reachable
from herdr's action menu.

In the overlay:

- arrows or `hjkl` move, `enter` jumps, `/` searches
- `esc` leaves search, then clears the filter, then closes; `q` or `ctrl+c`
  closes
- `s` cycles sort, `J`/`K` rearrange repos, `g`/`G` jump to the ends
- digits `1`-`9` jump to a ribbon row, `x` dismisses one until its status
  changes
- `o` marks the selected agent as the orchestrator
- `i` messages the orchestrator, `t` reports a finished agent to it (the repair
  key from the second paragraph)
- `S` installs the reporting skill, but only while its banner is on screen
- mouse: click a card to jump, click the banner to install the skill, wheel
  scrolls, hover highlights

There is no `?` binding and no in-app legend, so this list is the reference.

## Commands

```sh
muster open | jump orchestrator | jump previous
muster install [--key <letter>] [--no-keys] [--auto] | uninstall [--purge]
muster install-skill | uninstall-skill | uninstall-keys
muster mark-orchestrator
muster chain get [--json] | set <spec> [--independent a,b] [--by NAME] | clear
muster discover

musterd --ensure          # start a daemon if none is running, then exit
musterd --daemon          # run as the daemon (muster install spawns this)
musterd dump [--json]     # the supported way to read the snapshot
musterd status
```

There is nothing to configure. Muster reads no config of its own.

## Chain

The chain is the dependency order the orchestrator recorded, so a finished
agent shows who it unblocks. Two mechanisms, both owned by the orchestrator:

```sh
muster chain set "contracts > api > web,mobile" --independent infra
```

`>` is sequence, `,` is parallel. It persists in the state dir across sessions;
read it back with `muster chain get` to confirm or replace it. Without a chain
the gate rule stays silent rather than guessing.

The task lines come separately, through `herdr pane report-metadata`
(`task`, `blocked_on`, `note` tokens). The reporting skill teaches the
orchestrator to write them at dispatch time and rewrite them when the work
changes. Without it Muster falls back to terminal titles, then branch and
directory, labelled as guesses.

## How it works

Two binaries. `musterd` holds one event subscription on the herdr socket,
rebuilds state from `session.snapshot`, and writes `snapshot.json` into the
plugin state dir. `muster` is the overlay: one process per opening, a
`tea.Program` with alt screen that lives until `q`, reading the snapshot and
drawing. The daemon exists because opening this screen a hundred times a day
has to be free, and because something has to watch while the overlay is
closed. Reading and decoding the snapshot is all the overlay does at open
time; `go test ./internal/daemon -bench ReadSnapshot` measures it on your
machine.

Colour and border are pure hashes of the repo key, so those match on any
machine. Sigils are not: they are assigned round-robin in discovery order, so
no two repos collide until there are more repos than sigils. Grid slots are
pinned in `state.json` on first sight, so a repo keeps its cell and its look
for as long as your state file lives and the grid does not move under you.

`docs/herdr-api-notes.md` has the verified herdr mechanics the daemon is built
on, including why events are a hint and never a log.

## Uninstall

```sh
muster uninstall             # removes the key block and the skill, leaves state
muster uninstall --purge     # also deletes the state dir
herdr plugin unlink muster
```

`uninstall` removes the marked block from your herdr config (backup beside it),
removes `~/.claude/skills/muster-report/`, and reloads the config. Note the
skill goes with it even though installing it was opt-in.

It also records the refusal, so the startup hook does not rebind the keys at
the next herdr start. `--purge` deletes that record along with the rest of the
state dir, which is right when the plugin is going away.

## Troubleshooting

- `muster: no state directory` from a plain shell: run through herdr, or pass
  `--state-dir`. The README's install line assumes a herdr pane.
- The daemon is gone after herdr was down: by design it exits after 60s of an
  unreachable server and lives and dies with herdr. Re-run the install action
  or `musterd --ensure`.
- Linked the plugin and nothing happens: startup hooks do not fire on
  `plugin link` mid-session. Open the overlay once from herdr's action menu,
  which starts the daemon and binds the keys, or run the install action.
- Cards show terminal titles instead of task lines: the reporting skill is
  missing. Press `S` while its banner is up, or run `muster install-skill`.
- A new binding does nothing: an old overlay binary keeps running after a
  rebuild, so reopen it. A key you bound yourself is not the cause; install
  checks with `herdr config check` before writing and moves to a free letter
  rather than binding over you.

## Developing

```sh
go build -o ./bin/muster ./cmd/muster
go build -o ./bin/musterd ./cmd/musterd
go test ./... -race
herdr plugin link "$PWD"     # undo with: herdr plugin unlink muster
```

No toolchain manager required, here or anywhere else. `go.mod` is the only
place a Go version is written down, and CI reads it with
`setup-go: go-version-file`.

Testing needs a running herdr server with a few agents; launching the herdr TUI
from an agent session will hang it, so drive it from the CLI.

## License

MIT. See [LICENSE](LICENSE).
