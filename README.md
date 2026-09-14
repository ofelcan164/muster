# Muster

A [herdr](https://herdr.dev) plugin. One overlay, one keystroke, showing every
agent across every repo you have work in.

herdr's sidebar handles the straightforward case well. One workspace, one
agent, everything visible. That breaks down with many agents across many
repos, especially with an orchestrator handing work between them. Two flat
lists, workspaces here and agents there, with little visible state. No sorting
or searching, and no single place to land and see the whole picture.

Muster is that landing place. One tile per agent, and a dim one for each
workspace with no agent in it. Each agent tile shows what that agent is doing and how long it has sat quiet. Blocked ones show the
question they are asking. Finished ones stay visible until seen. The ribbon on
top pulls forward the ones that need attention now. Sort, search, rearrange,
and jump straight to the pick. The strip along the bottom keeps the
orchestrator and its last message in view, with keys to talk to it directly.

Task lines come from the reporting skill. It asks the orchestrator to write
down what each agent is doing at dispatch time, so the overlay shows real work
instead of guessing from terminal titles.

<!--
Future intro material, not ready yet. Revisit once this feels solid.
- The quiet finish story. An agent finishes, the orchestrator never
  learns, downstream work waits behind an open gate. Muster spots it
  and the t key reports it back.
- The dependency vision. Home base showing how work depends on other
  work, with chains as the recorded order.
-->

![The Muster overlay: a ribbon of the agents that need you, above one tile per
agent showing its status, age and task line, with the orchestrator's
last message along the bottom, beside herdr's own sidebar](docs/overlay.png)

## Install

Needs herdr 0.8.2 or newer and Go on `PATH`; herdr builds plugins from source. Full list
under [Requirements](#requirements).

```sh
herdr plugin install ofelcan164/muster
```

That is the whole install. There are no release tags yet, so this tracks the
default branch. Muster binds its keys itself: the startup hook runs
`muster install --auto`, which starts the daemon and writes the keybindings.
Installing mid-session gets no startup hook, so opening the overlay once from
herdr's action menu does the same job.

To keep the overlay and skip the global keys, use the **Remove Muster's
keybindings, keep the overlay** action, or run `muster install --no-keys`
before the first install. Either records the refusal in the state dir, and
`--auto` honours it from then on, so the startup hook stops putting them back.
The **Install Muster's keybindings** action asks for them again. (The
**Uninstall Muster's keybindings and skill** action is the full removal, skill
included.)

The actions matter here because `herdr plugin install` puts the binaries in
herdr's own checkout rather than on your `PATH`. Every `muster` command below
is reachable that way; from a source checkout you can run `./bin/muster`
directly instead.

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
beside it), a `muster badge` entry at the front of `tab_bar_right` in `[ui]`
that shows `◆ 3 need you · prefix+m` or `◆ 4 working · prefix+m`, a detached
daemon, that daemon's `musterd.log` and lock files in
the plugin state dir, and a `server.reload_config` call so the keys go live.
The daemon adds `snapshot.json` and `state.json` once it reaches herdr.
`ui.json` appears the first time you change the sort or dismiss a row, and
`chain.json` when you first set a chain.

The reporting skill is the exception. **Install the reporting skill for the
orchestrator** writes to your agent directories, so it stays a separate opt-in.
It puts one copy in `~/.agents/skills/muster-report/` and symlinks it into every
runtime you actually have: `~/.claude/skills`, `~/.codex/skills`
(`CODEX_HOME` honoured) and `~/.config/opencode/skill`. A runtime you have not
installed is left alone rather than created.

Commands that touch state (`install`, `chain`, `musterd dump`) run inside a
herdr pane, where `HERDR_PLUGIN_STATE_DIR` is set. From a plain shell they fail
with "no state directory": pass `--state-dir <dir>`.

## Requirements

- herdr 0.8.2 or newer. The manifest refuses anything older. 0.8.0 and 0.8.1
  were never tried
- Go on `PATH`, any version from 1.21. `go.mod` asks for 1.24 and the default
  `GOTOOLCHAIN=auto` fetches it, so an older Go still builds this. A Go with
  `GOTOOLCHAIN=local` set, which some distro packages do, needs 1.24 itself
- Linux or macOS
- Claude Code, Codex or OpenCode, only for the reporting skill

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

- arrows or `hjkl` move, `/` searches, `enter` jumps to the tile's agent or
  focuses an empty tile's workspace
- `esc` leaves search, then clears the filter, then closes; `q` or `ctrl+c`
  closes
- `s` cycles sort (first seen, a-z, attention, herdr); `J`/`K` move the
  selected tile's workspace one place in herdr's own order, and only do
  anything in herdr sort; `g`/`G` jump to the ends
- digits `1`-`9` jump to a ribbon row, `x` dismisses one until its status
  changes
- `o` marks the selected agent as the orchestrator
- `i` messages the orchestrator, `t` reports a finished agent to it, for when
  the orchestrator never heard it finish
- `S` installs the reporting skill, but only while its banner is on screen
- `M` jumps to the orchestrator. Muster opens as a herdr popup, which gets
  every key while it is open, your prefix included, so this is what keeps
  `prefix+shift+m` working. `prefix+m` does nothing inside; close with `q` or
  `esc`. `prefix+ctrl+m` arrives as `enter` and jumps to the selected tile
- mouse: click a tile to jump to it or focus its workspace, click the banner to install the skill, wheel
  scrolls, hover highlights. A click outside Muster does nothing: herdr keeps
  clicks outside a popup to itself

There is no `?` binding and no in-app legend, so this list is the reference.

## Commands

```sh
muster open | jump orchestrator | jump previous
muster install [--key <letter>] [--no-keys] [--auto] | uninstall [--purge]
muster install-skill | uninstall-skill | uninstall-keys
muster mark-orchestrator
muster doctor [--yes]     # check health, ask before restarting a stuck daemon
muster --help
muster chain get [--json] | set <spec> [--independent a,b] [--by NAME] | clear
muster discover
muster badge [letter]     # the tab bar line install writes

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

Who runs that: whoever has the binary and the state dir. An agent pane has
neither, so either you set it, or you give the orchestrator the full path to
`muster` and its `--state-dir`. The reporting skill deliberately does not teach
it, since a skill that names a path that does not exist on the reader's machine
is worse than one that stays quiet.

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
drawing. herdr shows it as a popup covering the tab area, not a pane in it,
so opening it changes nothing in the tab behind.
The daemon exists because opening this screen a hundred times a day has to be
free, and because something has to watch while the overlay is closed. Reading
and decoding the snapshot is all the overlay does at open time;
`go test ./internal/daemon -bench ReadSnapshot` measures it on your machine.

Colour is a pure hash of the repo key, so it matches on any machine. Sigils are
not: they follow the grid slot, skipping any mark already on screen, so two
repos on screen at once never carry the same one. Grid slots are pinned in
`state.json` on first sight. First-seen sort orders tiles by them, and a repo
keeps its sigil while the set of repos on screen stays the same.

`docs/herdr-api-notes.md` has the verified herdr mechanics the daemon is built
on, including why events are a hint and never a log.

## Updating

```sh
herdr plugin install ofelcan164/muster --yes
```

herdr has no update command, so installing again is the update. It pulls the
default branch and rebuilds `bin/`; `--ref <ref>` pins something else. The next
overlay you open runs the new build. The daemon checks its own binary every few
seconds and restarts itself on the new one, so nothing else needs running.

## Uninstall

```sh
muster uninstall             # removes the key block and the skill, leaves state
muster uninstall --purge     # also deletes the state dir
herdr plugin unlink muster
```

`uninstall` removes the marked block from your herdr config (backup beside it),
removes `~/.agents/skills/muster-report/` along with every runtime symlink
into it, and reloads the config. Note the skill goes with it even though
installing it was opt-in.

It also records the refusal, so the startup hook does not rebind the keys at
the next herdr start. `--purge` deletes that record along with the rest of the
state dir, which is right when the plugin is going away.

## Troubleshooting

- `muster: no state directory` from a plain shell: run through herdr, or pass
  `--state-dir`. herdr sets `HERDR_PLUGIN_STATE_DIR` for its own panes and
  plugin actions, and nothing else does.
- The daemon is gone after herdr was down: by design it exits after 60s of an
  unreachable server and lives and dies with herdr. Re-run the install action
  or `musterd --ensure`.
- Linked the plugin and nothing happens: startup hooks do not fire on
  `plugin link` mid-session. Open the overlay once from herdr's action menu,
  which starts the daemon and binds the keys, or run the install action.
- Muster closed without landing where you picked, or flashed and closed on
  open: the error is in `musterd.log` in the state dir. The popup is gone
  before anything it prints could be read.
- Wrong or empty after a reinstall (0 workspaces with agents up, stale tiles):
  run the **Check Muster's health** action. It restarts a daemon stranded on a
  deleted checkout or no longer writing its snapshot, and says how to fix
  anything else. It never touches the state dir. From a herdr pane in a source
  checkout, `./bin/muster doctor` does the same but asks first.
- Tiles show terminal titles instead of task lines: the reporting skill is
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

herdr reads `herdr-plugin.toml` when the plugin is linked, so a manifest change
(the popup's size, say) needs an unlink and a link. A rebuilt binary needs
nothing: the next open runs it.

No toolchain manager required, here or anywhere else. `go.mod` is the only
place a Go version is written down, and CI reads it with
`setup-go: go-version-file`.

Testing needs a running herdr server with a few agents; launching the herdr TUI
from an agent session will hang it, so drive it from the CLI. That covers less
than it used to: the popup has no pane id, so the CLI can open and kill it but
not read it or type into it. A server with no terminal attached also cannot show
whether a jump moves what you see, which is how a jump that landed nowhere
passed every headless check.

## License

MIT. See [LICENSE](LICENSE).
