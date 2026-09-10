# Muster

Herdr plugin. One overlay showing every agent across every repo: what each is
doing, which need attention, which finished unnoticed.

## Shape

`musterd` holds one herdr event subscription, rebuilds from
`session.snapshot`, writes `snapshot.json` to the plugin state dir. `muster`
is one process per overlay opening (`tea.Program`, alt screen, lives until
`q`), not per keypress.

```
cmd/muster            client: overlay, and every action the manifest binds
cmd/musterd           daemon: reconcile loop, dump, ensure

internal/herdr        socket client. Never shells out to the herdr binary
internal/daemon       reconcile loop, lifecycle, snapshot writing
internal/discover     workspace cwd to repo identity. Reads .git directly
internal/identity     colour/border hashed from repo key, sigil round-robin by slot
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
`ui.json` (repo order, sort, dismissed), `chain.json`, `musterd.log`,
`musterd.lock`.

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
muster install [--no-keys] | uninstall [--purge]    keybindings, and undoing them
muster install-skill | uninstall-skill              the orchestrator reporting skill
muster mark-orchestrator                            run on the orchestrator's pane
muster chain get [--json] | set <spec> [--independent a,b] [--by NAME] | clear
muster discover                                     rescan after workspace/worktree appears

musterd --ensure          start a daemon if none is running, then exit at once
musterd --daemon          run as the daemon
musterd dump [--json]     the supported way to read the snapshot
musterd status
```

Chain spec: `"contracts > api > web,mobile"`, `>` sequence, `,` parallel.
Task lines come via `herdr pane report-metadata` (`task`, `blocked_on`,
`note` tokens); the skill teaches the orchestrator to write them.

Global keys, once installed (`prefix` is the reader's herdr prefix key):
`prefix+m` overlay, `prefix+shift+m` orchestrator, `prefix+ctrl+m` back.

Overlay: arrows/`hjkl` move, `enter` jumps, `/` searches, `esc` leaves
search / clears filter / closes, `s` cycles sort, `J`/`K` rearrange, `g`/`G`
ends, `1`-`9` ribbon row, `x` dismisses one, `o` marks orchestrator, `i`
messages it, `t` reports a finished agent to it, `S` installs the skill while
its banner shows, `q`/`ctrl+c` closes. Mouse: click card jumps, click banner
installs skill, wheel moves, hover highlights.

## Gotchas

- Do not launch the herdr TUI from an agent session, it hangs. Drive the
  server from the CLI and read the overlay's own pane:
  `herdr pane list`, `herdr pane read <pane-id>`, `musterd dump`.
- herdr kills the pane's process group on close: the daemon needs `Setsid` to
  survive. Startup hooks do not fire on `plugin link` mid-session, so the
  install action starts the daemon too. Manifest commands resolve through
  `PATH`, not the plugin root, hence the leading `./`.
- Daemon exits after 60s of unreachable server, by design. A conflicting key
  is silently disabled, not rejected; `muster install` prints
  `herdr config check` when it disagrees.
- `install` writes a marked block + backup in the herdr config and calls
  `server.reload_config`. `uninstall` also removes
  `~/.claude/skills/muster-report/`. No config of Muster's own exists.
