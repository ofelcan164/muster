# Contributing

Build both binaries, run the tests:

```sh
go build -o ./bin/muster ./cmd/muster && go build -o ./bin/musterd ./cmd/musterd
go test ./... -race
```

Any Go from 1.21 will do. `go.mod` asks for 1.24 and `GOTOOLCHAIN=auto`, the
default, fetches it. A Go with `GOTOOLCHAIN=local` set, which some distro
packages do, needs 1.24 itself.

## Running from source

```sh
git clone https://github.com/ofelcan164/muster && cd muster
go build -o ./bin/muster ./cmd/muster
go build -o ./bin/musterd ./cmd/musterd
herdr plugin link "$PWD"     # undo with: herdr plugin unlink muster
```

`plugin link` does not run `[[build]]`, so build first. Manifest commands
resolve through `PATH` rather than the plugin root, so every one needs the
leading `./`. Startup hooks do not fire on `plugin link` mid-session: open the
overlay once from herdr's action menu, or run the install action.

herdr reads `herdr-plugin.toml` when the plugin is linked, so a manifest change
(the popup's size, say) needs an unlink and a link. A rebuilt binary needs
nothing: the next open runs it. The **Update Muster** action refuses a linked
checkout, so pull and rebuild it yourself.

In a source checkout the binaries are yours to run. From a herdr pane, which
sets the state dir, socket and session:

```sh
./bin/muster open | jump orchestrator | jump previous
./bin/muster install [--key <letter>] [--no-keys] [--auto] | uninstall [--purge]
./bin/muster install-skill | uninstall-skill | uninstall-keys
./bin/muster mark-orchestrator
./bin/muster doctor [--yes]   # asks before restarting a stuck daemon
./bin/muster update           # refuses a linked checkout
./bin/muster show [target]
./bin/muster chain get [--json] | set <spec> [--by NAME] | clear
./bin/muster discover         # what the workspace and worktree hooks run
./bin/muster badge [letter]   # what the tab bar entry runs
./bin/musterd --ensure        # start a daemon if none is running, then exit
./bin/musterd dump [--json]   # the supported way to read the snapshot
./bin/musterd status
```

From a plain shell they fail with "no state directory": pass `--state-dir <dir>`
before the command, and set `HERDR_SESSION=<name>` to read a named session
rather than the default one.

## Five things that otherwise cost an afternoon

- **Rebuild before you look.** herdr runs what is in `bin/`, so an overlay
  change you have not built is one you will not see.
- **Restart the daemon after building `musterd`.** The running one is still the
  old one: `pkill -f 'musterd --daemon'`, then `./bin/musterd --ensure`.
- **Muster needs a state directory.** In a herdr pane it comes from
  `HERDR_PLUGIN_STATE_DIR`; from a plain shell, pass `--state-dir <dir>` and
  point it somewhere scratch, because `install` edits your real herdr config.
- **Do not start the herdr TUI from an agent session**, it hangs. Drive the
  server from the CLI: `herdr pane list`, `herdr pane read <pane-id>`,
  `musterd dump`.
- **Do not commit a `.mise.toml`.** A toolchain pin in the repo root is not
  scoped to your shell: `herdr plugin install` builds in this directory, and if
  herdr resolves `go` through a mise shim, the pin is what the shim obeys.
  Pinning a version the user has not got turns `plugin install` into a silent
  multi-minute toolchain download. Keep a personal pin in `.mise.local.toml`,
  which is gitignored. `go.mod` is the version of record.

## Testing against herdr

Testing needs a running herdr server with a few agents. A named session keeps
that away from your real one: `herdr --session scratch server` starts one
headless, `herdr --session scratch <command>` drives it, and Muster keeps its
state for it in `sessions/scratch/`. Read it with
`HERDR_SESSION=scratch ./bin/musterd --state-dir <dir> dump`.

That covers less than it used to: the popup has no pane id, so the CLI can open
and kill it but not read it or type into it. A server with no terminal attached
also cannot show whether a jump moves what you see, which is how a jump that
landed nowhere passed every headless check.

CI runs build, vet and `test -race` on Linux and macOS, with `setup-go` reading
`go-version-file: go.mod`.

## How it works

Two binaries. `musterd` holds one event subscription on the herdr socket,
rebuilds state from `session.snapshot`, and writes `snapshot.json` into the
plugin state dir. `muster` is the overlay: one process per opening, a
`tea.Program` with alt screen that lives until `q`, reading the snapshot and
drawing. herdr shows it as a popup covering the tab area, not a pane in it, so
opening it changes nothing in the tab behind.

The daemon exists because opening this screen a hundred times a day has to be
free, and because something has to watch while the overlay is closed. Reading
and decoding the snapshot is all the overlay does at open time;
`go test ./internal/daemon -bench ReadSnapshot` measures it on your machine.
It exits after 60s of an unreachable herdr server, by design, and restarts
itself when an update rebuilds its binary.

Each herdr session gets a daemon, snapshot, sort and usual order of its own.
herdr hands every session the same plugin state dir, so a named session keeps
its files in `sessions/<name>/` inside it, and the default session keeps the
top level.

Colour is a pure hash of the repo key, so it matches on any machine. Sigils are
not: they follow the grid slot, skipping any mark already on screen, so two
repos on screen at once never carry the same one. Grid slots are pinned in
`state.json` on first sight. No sigil is round or shaped like a status icon, so
a repo's mark never reads as a state.

Keybindings: if a letter is taken, `install` renders each candidate into a
throwaway config and reads `herdr config check` before writing, so it never
binds over the user. The chosen letter is read back from the marked block on
later runs, so editing `key = "prefix+X"` there and rerunning install moves it.

The orchestrator's usual order between repos lives in `chain.json`
(`muster chain set "contracts > api > web,mobile"`, `>` sequence, `,`
parallel). It is a default the orchestrator reads before writing `depends_on`,
and Muster draws nothing from it.

`docs/herdr-api-notes.md` has the verified herdr mechanics the daemon is built
on, including why events are a hint and never a log.
