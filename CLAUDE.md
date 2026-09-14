# Muster

Herdr plugin. One overlay showing every agent across every repo: what each is
doing, which need attention, which finished unnoticed.

## Shape

`musterd` holds one herdr event subscription, rebuilds from
`session.snapshot`, writes `snapshot.json` to the plugin state dir. `muster`
is one process per overlay opening (`tea.Program`, alt screen, lives until
`q`), not per keypress. herdr opens it as a popup, not a pane.

```
cmd/muster            client: overlay, and every action the manifest binds
cmd/musterd           daemon: reconcile loop, dump, ensure

internal/herdr        socket client. Never shells out to the herdr binary
internal/daemon       reconcile loop, lifecycle, snapshot writing
internal/discover     workspace cwd to repo identity. Reads .git directly
internal/identity     colour hashed from repo key, sigil by slot, skipping any
                      mark already on screen
internal/chain        orchestrator-recorded dependency order (chain.json)
internal/triage       attention ranking that fills the ribbon
internal/state        state dir, lock, atomic writes. No invented fallback:
                      HERDR_PLUGIN_STATE_DIR or --state-dir, else ErrNoStateDir
internal/install      the only code that writes files the user owns
internal/model        snapshot types both sides share
internal/ui           overlay
```

`internal/ui` splits by concern: `model` (lifecycle and dispatch), `targets`
(what the selection can land on), `keys`, `mouse`, `filter`, `order`, `strip`
(the orchestrator strip), `talk` (the `i` and `t` keys), `view`, `theme`.

State dir files: `snapshot.json`, `state.json` (grid slots, learned state),
`ui.json` (sort, dismissed) and `ui.json.lock` (serialises two overlays
saving it), `chain.json`, `keys.optout`, `musterd.log`, `musterd.lock`,
`musterd.spawn.lock` (serialises concurrent `--ensure`).

## Build and test

```
go build -o ./bin/muster ./cmd/muster && go build -o ./bin/musterd ./cmd/musterd
go test ./... -race
```

Manifest builds both into `bin/`. Rebuild after touching the overlay or the
live session keeps running the old one.

## Commands

```
muster open | jump orchestrator | jump previous     what the keybindings invoke
muster install [--key <letter>] [--no-keys] [--auto]        keybindings
muster uninstall-keys | uninstall [--purge]         undoing them
muster install-skill | uninstall-skill              the orchestrator reporting skill
muster mark-orchestrator                            run on the orchestrator's pane
muster chain get [--json] | set <spec> [--independent a,b] [--by NAME] | clear
muster discover                                     make sure the daemon is up; the event hooks call it
muster badge [letter]                               the tab_bar_right line install writes

musterd --ensure          start a daemon if none is running, then exit at once
musterd --daemon          run as the daemon
musterd dump [--json]     the supported way to read the snapshot
musterd status
```

Chain spec: `"contracts > api > web,mobile"`, `>` sequence, `,` parallel.
Task lines come via `herdr pane report-metadata` (`task`, `blocked_on`,
`note` tokens); the skill teaches the orchestrator to write them.

Global keys, once installed (`prefix` is the reader's herdr prefix key):
`prefix+m` overlay, `prefix+shift+m` orchestrator, `prefix+ctrl+m` back. The
letter falls back through `m g u y` when the user already bound one, and
`--key` overrides.

Overlay: one tile per agent, one dim lowercase tile per workspace with no
agents. arrows/`hjkl` move, `enter` jumps to the tile's pane or focuses its
workspace, `/` searches, `esc` leaves search / clears filter / closes, `s`
cycles sort (first seen, a-z, attention, herdr), `J`/`K` move the selected
tile's workspace one place in herdr's own order and only do anything in herdr
sort, `g`/`G` ends, `1`-`9` ribbon row, `x` dismisses one, `o` marks
orchestrator, `i` messages it, `t` reports a finished agent to it, `S`
installs the skill while its banner shows, `q`/`ctrl+c` closes, `M` jumps to
the orchestrator.
Mouse: click a tile jumps or focuses its workspace, click banner installs
skill, wheel moves, hover highlights.

## Gotchas

- Do not launch the herdr TUI from an agent session, it hangs. Drive the
  server from the CLI: `herdr pane list`, `musterd dump`. The popup has no
  pane id, so `herdr pane read` cannot see Muster; headless you can only open
  it with `herdr plugin action invoke muster.open` and kill it.
- The popup is modal: herdr sends it every key before its own bindings,
  prefix included, so the global keys cannot fire while it is open. Muster
  ignores the prefix and binds plain `M`, which keeps `prefix+shift+m`
  working. `prefix+m` does nothing inside (only `q` and `esc` close), and
  `prefix+ctrl+m` arrives as `enter` (0x0D).
  Alt keys are dropped from the search and message inputs, or an `alt+q`
  prefix types a q. One popup per session: a second open is `ui_busy`, which
  `muster open` treats as done.
- herdr kills the pane's process group on close: the daemon needs `Setsid` to
  survive. Startup hooks do not fire on `plugin link` mid-session (still true
  on 0.9.0), so the install action and the overlay both bind the keys too.
  Manifest commands resolve through `PATH`, not the plugin root, hence the
  leading `./`.
- Daemon exits after 60s of unreachable server, by design.
- Reinstalling rebuilds `bin/` but runs no hook, and `--ensure` only checks the
  lock, so the daemon watches its own binary on the 5s tick and execs the new
  one once it has looked the same twice. Without that an old daemon kept
  writing an old-shape snapshot under a new overlay for days.
- herdr disables a conflicting key silently rather than rejecting it, and the
  managed block is appended last, so a collision always disables Muster's. So
  `install` asks first: it renders each candidate into a throwaway
  `XDG_CONFIG_HOME` and reads `herdr config check` for
  `prefix+X: kept ..., disabled ...`. The chosen letter is read back out of the
  block on later runs, which is what makes `--key` survive the startup hook.
  Exhausting the candidates is an error, never a keyless install.
- `install` writes a marked block + backup in the herdr config and calls
  `server.reload_config`. `uninstall` also removes
  the skill. No config of Muster's own exists.
- The skill is one canonical copy at `~/.agents/skills/muster-report/`, symlinked
  into each runtime that exists (`~/.claude/skills`, `~/.codex/skills` honouring
  `CODEX_HOME`, `~/.config/opencode/skill`), relative target computed with
  `filepath.Rel`. The embedded bytes are the version, so there is no sentinel
  file: a differing copy is simply rewritten. A plain directory from an older
  install gets replaced by the link.
- The `[[startup]]` hook is `muster install --auto`, so setup is just
  `herdr plugin install`. `--auto` skips when `keys.optout` is in the state
  dir; `--no-keys` and both uninstalls write that marker, plain `install`
  clears it. Without it every uninstall would undo itself at the next start.
- `install` also puts a `muster badge` entry at the front of `[ui]
  tab_bar_right`. TOML allows one `[ui]` and one `tab_bar_right`, so when the
  user has them the entry goes into their own array, outside the marked block,
  and `Remove` takes back that exact entry. herdr runs tab bar commands through
  `/bin/sh` on the server with no plugin env, so the entry carries absolute,
  single-quoted paths and `--state-dir`. No state dir means no badge.
