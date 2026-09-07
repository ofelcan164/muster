# Muster handoff

For whoever picks this up next. Written 2026-09-07, after phase 1.

Read `docs/plan.html` for the design and `docs/herdr-api-notes.md` for the
mechanics. This file is what neither of those can tell you: what got built, what
is wrong with it, and which decisions were made deliberately so you do not undo
them by accident.

## State of things

`musterd` and the overlay both work against a live herdr 0.8.2 session. 127
tests, race-clean, 30 commits on `master`. The keybindings are installed in the
user's config and verified working.

```
alt+q m          open Muster, press again to close
alt+q shift+m    jump to the orchestrator
alt+q ctrl+m     back to the previous agent
```

Phase 1 as the plan defines it is done. Phase 2 has not started.

## What the packages do

| Package | Job |
|---|---|
| `internal/herdr` | Socket client. RPC plus the long-lived event subscription. Never shells out to the herdr binary. |
| `internal/discover` | Workspace cwd to repo identity. Reads `.git` directly rather than forking git. |
| `internal/identity` | Colour, sigil and border from the repo key. |
| `internal/chain` | The orchestrator-recorded dependency order. |
| `internal/triage` | The attention ranking. |
| `internal/daemon` | Reconcile loop, lifecycle, `dump`. |
| `internal/state` | State dir, lock, atomic writes, everything persisted. |
| `internal/install` | The only code that writes to a file the user owns. |
| `internal/ui` | The overlay. |

## Five things to know before changing anything

**Events are a hint, never a log.** herdr replays the session's whole event
history on every subscribe, at ten events a second, including events for panes
that no longer exist and in an order that is not causal. Every event does one
thing: schedule a reconcile. State is always rebuilt from `session.snapshot`.
Anything that folds events into state will be subtly wrong and will look fine
in testing.

**Subscribe only to globally-scoped event types.** Per-pane types need a
`pane_id`, and a second `events.subscribe` on a live connection makes the server
reset it, so a per-pane subscription could never be extended when a new pane
appears. `pane.updated` is global and carries the whole pane record, which
covers the same ground.

**`pane.read` costs 350ms; `pane.process_info` costs 0.8ms.** That ratio drove
several decisions. Output scanning was removed entirely. The blocking question
is read only for blocked agents and cached until they stop being blocked. Never
put a `pane.read` on the reconcile path.

**herdr exposes no transition timestamp anywhere.** I searched the whole schema.
The daemon measures status age itself and persists it. `AgeKnown` is false when
the daemon found an agent already in a status rather than watching it get there,
and that distinction matters: a lower bound is fine for a threshold ("idle at
least ten minutes") and wrong for ordering ("longest waiting first"). Both cases
are handled explicitly in `internal/triage`. Do not collapse them.

**Nothing is hardcoded about any repo.** Names, colours, sigils and grid slots
are all derived at runtime and stored in Muster's own state dir. Grid slots come
from workspace-number order rather than map order, so a fresh install lays out
the same way twice.

## Decisions that were deliberate

Do not "fix" these without asking. Each one was chosen against a real
alternative.

- **Search is `/`, not any letter.** The plan had any bare letter start
  filtering, which forced the rule that vim keys only work while the query is
  empty and left no letter free for anything else. `/` follows herdr's own
  convention and costs one keystroke.
- **Every repo gets a card, ordered with agents first.** This went back and
  forth. Hiding quiet repos was tried and reverted: the user wants them visible,
  just never above something they are working in.
- **The chain is owned by the orchestrator, not the user.** Written with
  `muster chain set`, persisted, read back with `muster chain get` so a new
  session can confirm it. There is deliberately no user-wins precedence.
- **The gate rule stays silent without a chain.** Guessing what "downstream"
  means produced rows about unrelated repos, and a ribbon row you learn to
  distrust is worse than an empty ribbon.
- **A blocked orchestrator does reach the ribbon**, even though the plan gives
  it its own strip. Being blocked is a request for you specifically.
- **The snapshot is a private cache.** `musterd dump --json` is the supported
  interface. The file shape is free to change.
- **`musterd` refuses to run without a state dir.** It will not invent one.
  herdr supplies it; `--state-dir` is for running by hand.

## Answered questions worth not re-deriving

Both of these cost real time to establish and are recorded in
`docs/herdr-api-notes.md` with the evidence.

- Mouse coordinates from herdr are **pane-local and 0-based**. Measured with a
  real mouse against a pane at screen `x=97` inside a tab area starting at
  column 26; nothing in Muster needs an offset correction. Hover was never
  broken.
- A `plugin_action` keybinding **cannot** open a `[[panes]]` entrypoint. Only
  `[[actions]]` ids resolve. `prefix+m` binds to an action that shells out.
- There is **no global per-token sidebar style table**. Styles must be inline in
  each row, and custom tokens keep their `$`.
- Manifest `[[events]]` names are **dotted**, not underscored. The plan's
  manifest had this wrong and herdr only warns.
- `herdr config check` **detects key conflicts** and reports which binding wins,
  but does **not** resolve action ids, so a binding pointing at a non-existent
  action validates cleanly and then does nothing.
- There is **no plugin uninstall hook**. `muster uninstall` exists because
  nothing will call us on unlink.

## Known problems

Nothing open. Everything phase 1 shipped with is either fixed or was measured
and found not to be a problem. See the git log from `ca3695e` on.

## What is not built

- Phase 3 entirely: sidebar tokens and generated config, `agent.view.set`
  sorting, the chain strip, notifications, notes on `n`.
- Tier 3 of the fallback ladder is **cancelled**, not pending. The user chose to
  skip per-repo Stop hooks. `taskFor` still reads a `self_task` token, so the
  rung works if anything ever writes one; nothing does, and the reporting skill
  deliberately does not mention it.

Phase 2 is done. Tiers 1, 2, 4 and 5 of the ladder and gate detection turned out
to have been built in phase 1 already, whatever this file said. What was
genuinely missing was the reporting skill and its installer, the orchestrator
strip, and the `i` and `t` keys.

`muster install-skill` writes the skill and `muster uninstall-skill` removes it;
`muster uninstall` calls the latter. It has never been run against the user's
real `~/.claude/skills/`, only a temp directory. That is theirs to run.

## Decisions waiting on the user

From `docs/questions-2.html`, which is deliberately untracked. Do not commit the
HTML docs; the user asked for them to stay out of git.

- Whether the reporting skill may be installed into `~/.claude/skills/`. The
  answer was skill yes, `CLAUDE.md` no. The plan for the missing instruction is
  to lean on the skill's own `description` frontmatter instead.
- Sidebar tokens and generated config were approved, conditional on
  `muster uninstall` leaving nothing behind. That command exists and is tested.
- Notifications: leave the user's herdr toast settings alone.

## Testing notes

There is a running herdr server with two real agents. Five throwaway git repos
live in `/tmp/muster-fixtures` with workspaces on them, created to prove
discovery handles org collisions and missing remotes. They are clutter but
useful; the user asked to keep them until phase 2 is done.

**Do not launch the TUI from an agent session.** It will hang. Drive the server
from the CLI and read the overlay's own pane with `herdr pane read`.

**Ask before prompting the user's agents.** They cost the user tokens.

Two traps worth knowing:

- `herdr pane send-keys` silently ignores an invalid key name. `ctrl-c` does
  nothing; `ctrl+c` works.
- lipgloss strips all styling when it cannot detect a colour-capable terminal,
  which a test binary never has. `internal/ui/ui_test.go` forces the profile in
  `init`. Without that, any test asserting on styling silently measures nothing
  and passes.

## A process warning

Three separate bugs in this repo came from scripted string replacement against
Go source that silently matched nothing after `gofmt` realigned it. One shipped
a wrong API field name into a binary. Use edits that fail loudly.

Two more came from `git add` with a broad path sweeping unrelated work, and once
untracked documentation, into a commit. Stage explicit paths.
