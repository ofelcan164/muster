# herdr API notes

Verified against the installed 0.8.2 binary and a live server on 2026-09-05,
with the subscription section rechecked against a live 0.9.0 server on
2026-09-10. This file is only the mechanics needed to write code.

Muster's manifest sets `min_herdr_version = "0.8.2"`. That is the version this
was verified against, not a proven floor: 0.8.0 and 0.8.1 were never tried, and
they may well work. Treat it as "tested here" rather than "required".

## Getting the truth

The binary is the authority, not the website.

```
herdr --help          # top-level
herdr <group>         # any group with no subcommand prints its usage
herdr api schema --json > schema.json   # full request/response JSON schema
herdr api snapshot    # live session state
```

## Undocumented, cost real time

1. **Manifest commands resolve through `PATH`**, not relative to the plugin
   root, even though the pane's working directory *is* the plugin root.
   `bin/muster` fails with `no viable candidates found in PATH`.
   Use `./bin/muster`.

2. **`plugin pane close <self>` kills the process immediately.** Nothing after
   that line runs. Do all work first.

3. **herdr kills the pane's process group on close.** A plain forked child dies
   with the pane. Anything that must outlive it needs `Setsid`
   (`syscall.SysProcAttr{Setsid: true}`). This is how the daemon must start.

4. **There is no way to ship a prebuilt binary.** The manifest schema, read
   out of the 0.9.0 binary on 2026-09-10, is exactly `id`, `version`,
   `min_herdr_version`, `description`, `platforms`, `build`, `startup`,
   `actions`, `events`, `panes`, `link_handlers`. No asset or release field.
   `plugin install` clones into a managed checkout and runs `[[build]]` there,
   so a Go toolchain on the user's machine is a hard requirement and goreleaser
   would not change that.

   The build runs in the plugin root, which is why a tracked `.mise.toml` was a
   trap: herdr's PATH carries mise's shims but not the tool bin dirs, so `go`
   resolves to the shim, and the shim obeys whatever the repo pins. Measured
   here at over two minutes of silent toolchain download.

5. **Startup hooks do not fire on `plugin link` mid-session.** They fire after
   session restore on server start, and again on `herdr update --handoff`.
   So the daemon must be startable by the install action too, and `--ensure`
   must be idempotent under a lock. Re-checked on 0.9.0 on 2026-09-10 by
   relinking and reading `herdr plugin log`: no new startup entry. This is
   also why the overlay binds the keys on first open, for a plugin installed
   into a running session.

## Popup panes

How Muster opens since 2026-09-11. Verified on 0.9.0 that day against a
headless server and the v0.9.0 source.

- Popup plugin panes exist since 0.7.4 (CHANGELOG, #1125), below Muster's
  `min_herdr_version`.
- The manifest takes `placement = "popup"` with `width` and `height`, as cells
  (an integer) or a percentage string like `"90%"`. At 90% on a 120x40 tab
  area the program got 105x34.
- Not part of any tab. `pane list` does not show it and the layout does not
  change, so there is no zoom and no focus restore on exit. While it is open,
  `session.snapshot`'s `focused_pane_id` is the pane underneath.
- One popup per session. A second `plugin.pane.open` returns `ui_busy` ("a
  popup pane is already open", `src/app/api/plugins/mod.rs:478`), and
  `muster open` counts that as done.
- A successful open returns `{"result":{"type":"ok"}}`, not the `plugin_pane`
  object an overlay open returns. The `focus` param is never read for a popup.
- Jump with `pane.focus`, not `agent.focus`. See "Jumping out" below: on
  0.9.0 `agent.focus` moves the session's focus but not what an attached
  terminal shows. A headless server has no attached terminal, so a headless
  check cannot tell the two apart; this one needed the real client.
- It covers the tab area only. The client lays it out inside the pane surface
  (`src/client/shell/composition.rs:470`), so the sidebar stays visible and
  100% is the whole tab area, never the screen.
- No dimmed backdrop. herdr's own popups (settings, help) dim everything
  behind them (`src/client/shell/overlays.rs:47`), but the plugin popup path
  only clears its own rectangle and draws a border. No manifest field changes
  that on 0.9.0.
- The process gets `HERDR_SOCKET_PATH`, `HERDR_PLUGIN_STATE_DIR` and the rest
  of the plugin env, but no `HERDR_PANE_ID`, `HERDR_TAB_ID` or
  `HERDR_WORKSPACE_ID`. It has no pane id at all, so `herdr pane read` and
  `pane send-text` cannot reach it. Headless, the most you can do is start it
  with `plugin action invoke muster.open` and kill it.
- It is modal. The client routes every key to the popup before its own
  bindings, prefix included (`src/client/shell/input.rs:503`), and drops clicks
  outside it (`src/client/shell/mouse.rs:855`) rather than delivering them, so
  the program cannot close on a click outside. Direct bindings like
  `alt+enter` do nothing while it is open. Mouse events inside go through the
  same translation panes use.
- Keys arrive in legacy encoding unless the program asks for the kitty
  protocol, which bubbletea v1 does not. So `alt+q` is `ESC q`, and bubbletea
  reports it as `alt+q`. `ctrl+m` is 0x0D, the same byte as Enter, and
  arrives as `enter`. `ctrl+i` arrives as `tab`. `ctrl+h` is 0x08, which
  bubbletea names `ctrl+h`, not `backspace`.

## Overlay panes

How Muster opened before the popup. Confirmed by spike:

- Get a real TTY. `stdin` is a char device.
- Get an initial size and live resizes. Bubbletea saw `86x33 -> 76x23 -> 47x21`
  while the window was dragged.
- **Do** get `HERDR_PANE_ID`. The docs only rule that out for popups.
- Appear in `pane list` with the manifest's `title` as `label`, which is how to
  find your own pane without the env var.
- Sized to the tab area, i.e. terminal width minus the sidebar.

**Answered 2026-09-07: mouse coordinates arrive pane-local and 0-based.**
This was the open question the whole click and hover story rested on, and a
unit test could never answer it: the handler was being called with coordinates
the model itself produced.

Measured by opening the overlay through `plugin.pane.open` with
`env.MUSTER_MOUSE_DEBUG` set, then moving a real mouse across it. 487 events
against a pane herdr reported at screen `x=97 y=1`, inside a tab area starting
at screen column 26:

```
view 142x40    x range 0..141    y range 0..20
123 events below x=26     0 events at or above x=142     y reaches 0, never 40
```

Screen-absolute coordinates would have started at 26 and run to 167, and y
would have started at 1. So herdr translates, the same as tmux does, and no
offset correction belongs in Muster.

The 141 events that resolved to nothing are all geometry working correctly:
they land on the column separators, on the columns integer division leaves over
at the right edge (`x=140,141` for a 142-wide three-column grid), on the header
and section rules, or below the last card.

## Jumping out of an overlay

`pane.focus {"pane_id": ...}` then exit the process. That is the whole thing.

`agent.focus` was the call until 2026-09-11, and on 0.9.0 it is wrong. 0.9.0
gave every attached client its own view of which workspace and tab it shows
(CHANGELOG, #3526), and a public socket call only moves those views for
`workspace.focus`, `tab.focus` and `pane.focus`
(`src/server/headless/client_views.rs:811`). `agent.focus` moves the session's
focus and leaves every screen where it was, so from the popup a jump closed
Muster and landed nowhere. `pane.focus` exists on 0.8.2 too, and takes shells
and editors, which `agent.focus` refused with `agent_not_found`.

- ~2ms, works across workspaces.
- The overlay's own focus restore does **not** steal focus back.
- No `plugin pane close` needed. No detached helper. No delay.
- Overlay leaves no residue in `pane list`.

But an overlay restores the focus and zoom it saved at open time when its
process exits (`restore_overlay_after_exit`, `src/app/api.rs:458`). It is a
real split in the tab it opened from, zoomed. Jump to an agent in that same tab
and you land on it, but the tab stays zoomed (`pane layout` shows
`"zoomed": true`), hiding the tab's other panes. That, and a second overlay
opening in another tab while the first was still up, is why Muster became a
popup. See "Popup panes".

## Response shapes

Everything is `{"id": ..., "result": {...}}` or `{"id": ..., "error": {...}}`.
Nesting bit me once, so:

```
agent list      -> .result.agents[]        {pane_id, agent, agent_status, name,
                                            workspace_id, tab_id, cwd, focused,
                                            terminal_title_stripped, state_change_seq}
pane list       -> .result.panes[]         {pane_id, label, scroll.viewport_rows}
pane layout     -> .result.layout          {area{x,y,width,height}, focused_pane_id,
                                            panes[]{pane_id, rect, focused}}
api snapshot    -> .result.snapshot        {focused_pane_id, focused_workspace_id,
                                            focused_tab_id, workspaces[], agents[]}
plugin pane open-> .result.plugin_pane.pane.pane_id
pane read      -> .result.read.text        also .revision, .truncated
session.snapshot-> .result.snapshot         panes[] and agents[] both carry
                                            .tokens and .state_labels
```

Two things about workspaces that cost me a wrong assumption:

- **A workspace has no `cwd` field.** `WorkspaceInfo` is `{workspace_id, label,
  number, active_tab_id, agent_status, focused, pane_count, tab_count, tokens,
  worktree}`. To get a workspace's directory, read it off its panes. Taking the
  most common pane `cwd` beats taking the first, which may be a shell someone
  has `cd`'d out of.
- **`workspace.worktree` was null for every workspace I made with
  `workspace create --cwd <a git repo>`**, so it cannot be relied on for the
  branch. `worktree list` also fails outright with `not_git_worktree` when the
  focused workspace is not in a work tree. Reading `.git/HEAD` and
  `.git/config` directly is both more reliable and cheaper than either.

`pane layout` `area` is the tab area, so `area.x` is the sidebar width and
`area.width` is what an overlay actually gets.

## In the CLI

`herdr pane report-metadata <pane_id> --source ID [--title T] [--token K=V]
[--state-label STATUS=TEXT] [--ttl-ms N] [--clear-token K]`

This is the context-writing contract. `--ttl-ms` maxes at 86400000. Tokens are
capped at 16 per pane, keys `^[A-Za-z0-9_-]{1,32}$`.
`herdr workspace report-metadata` is the same for workspaces.

`herdr agent {list,get,read,focus,prompt,rename,wait,explain}`,
`herdr pane {read,send-keys,wait-output,...}`, `herdr worktree list`
(gives `repo_root`, `checkout_path`, `is_linked_worktree`, `open_workspace_id`).

## Socket only, no CLI

`agent.view.set` / `agent.view.clear`. Reshapes herdr's own agent list with a
filter tree (`all`/`any`/`not`/`eq`/`in`/`exists`) over builtin fields
(`status`, `workspace_id`, `tab_id`, `pane_id`, `agent`, `seen`,
`state_change_seq`) or `{"token": "name"}`, plus a sort including `attention`.
Send raw JSON to `$HERDR_SOCKET_PATH`.

## Events

Subscribe over the socket. Full set:

```
pane_created pane_closed pane_exited pane_focused pane_moved pane_updated
pane_output_changed pane_agent_detected pane_agent_status_changed
tab_created tab_closed tab_focused tab_moved tab_renamed
workspace_created workspace_closed workspace_focused workspace_moved
workspace_renamed workspace_reordered workspace_updated workspace_metadata_updated
worktree_created worktree_opened worktree_removed
layout_updated
```

`pane_output_changed` and `pane_agent_status_changed` are high volume. Manifest
`[[events]]` spawns a process per event and there is a concurrency cap that
returns `plugin_command_limit_reached`, so only declare rare events there.

### Manifest `[[events]]` names are dotted too

`on = "workspace_created"` is silently wrong. `herdr plugin list` reports it as
`warning: unknown event 'workspace_created'` and the hook never fires. `plugin
link` still succeeds, so nothing fails loudly. Use `on = "workspace.created"`.

The warning only shows in `plugin list` output, so check there after linking.

### Holding a subscription (verified 2026-09-06 on 0.8.2, 2026-09-10 on 0.9.0)

`events.subscribe` is the long-lived one. `events.wait` is a one-shot match with
a timeout and is not a subscription at all. Subscribe replies
`{"result":{"type":"subscription_started"}}` and then streams events on the same
connection. Live delivery measured at 23 to 26ms.

Five things that shape any daemon built on this.

1. **Subscription names are dotted, delivered names are underscored.**
   You subscribe to `pane.updated` and receive `pane_updated`. The list above is
   the delivered form. The exception is `pane_agent_status_changed`, which comes
   back dotted as `pane.agent_status_changed`. Normalise both.

2. **On 0.8.2, every subscribe replays the session's whole event history first,
   paced at one event per 100ms.** The replay includes events for panes and
   workspaces that have since closed, and it is not causally ordered: I saw
   `pane_closed` for `w1:p2` arrive before that pane's `pane_created`. Two
   subscribes a second apart replayed byte-identical sequences. Expect ten
   redundant wakeups a second while a replay drains.

   **0.9.0 removed the replay.** Measured 2026-09-10: I created a workspace and
   closed it, then opened a fresh subscription to all 16 global types. It
   returned `subscription_started` and nothing else, twice over. The same
   connection then delivered `pane_created`, `workspace_created`, `tab_created`
   and `pane_updated` for the next workspace I made, so the subscription was
   live, just not retrospective. The 0.9.0 docs say the same thing and prescribe
   the ordering that closes the bootstrap gap: subscribe on one connection, wait
   for the acknowledgement, buffer the stream, then call `session.snapshot` on
   another. Muster already did that, so the code needed no change.

   Either way the stream is a "something changed" hint and nothing more. Build
   state from `session.snapshot`, never from the events.

3. **A second `events.subscribe` on a subscribed connection resets it.** The
   server closes the socket. Subscriptions are fixed for the life of a
   connection, so a new pane cannot be added to an existing subscription, and
   RPCs need their own connection.

4. **`pane.agent_status_changed`, `pane.output_matched` and
   `pane.scroll_changed` require a `pane_id`.** There is no global agent-status
   subscription. One invalid entry rejects the whole batch with
   `invalid_request` and an empty `id`, and no events follow.

5. **`pane.updated` is global and carries the full pane record**, including
   `agent`, `agent_status`, `cwd`, `terminal_title_stripped` and `revision`.
   That covers every per-pane signal, so subscribing globally to `pane.updated`
   avoids (3) and (4) entirely. This is the one worth knowing: it removes the
   need for per-pane subscriptions and the resubscribe-per-pane churn they force.

Polling is not needed. Subscriptions work.

## Agent state

`idle` `working` `blocked` `done` `unknown`.

`done` is idle after work you have not seen. Focusing marks seen.
**CLI reads do not mark seen**, so a daemon can poll `pane read` and
`agent read` continuously without consuming the unseen signal.

Confirmed 2026-09-06: an agent left in `done` stayed `done` across four
`pane read` calls over 32 seconds.

**`pane read` is slow: 330 to 375ms per call**, against 2.2ms for
`session.snapshot`. It is by far the most expensive thing in the API. Reading
seven agent panes inline costs about two and a half seconds, so output scanning
has to run off whatever path keeps state fresh. Doing it inside musterd's
reconcile pushed the first snapshot after startup from 15ms to 1054ms.

## Env in plugin commands

```
HERDR_ENV=1  HERDR_SOCKET_PATH  HERDR_BIN_PATH
HERDR_PLUGIN_ID  HERDR_PLUGIN_ROOT  HERDR_PLUGIN_CONFIG_DIR  HERDR_PLUGIN_STATE_DIR
HERDR_PANE_ID  HERDR_WORKSPACE_ID  HERDR_TAB_ID
HERDR_PLUGIN_ENTRYPOINT_ID   (pane commands)
HERDR_PLUGIN_ACTION_ID       (actions)
HERDR_PLUGIN_EVENT / _JSON   (startup and event hooks)
HERDR_PLUGIN_CONTEXT_JSON
```

Always call herdr through `HERDR_BIN_PATH`, not a bare `herdr`.

**Answered 2026-09-14: sessions share a plugin state dir.** A plugin linked
into a throwaway config got the same `HERDR_PLUGIN_STATE_DIR` from its startup
hook in a named session and in the default one. In a named session herdr sets
`HERDR_SESSION=<name>` for panes, plugin commands and `tab_bar_right` commands
alike, next to that session's `HERDR_SOCKET_PATH`, and leaves it unset in the
default session. Neither server process carries it in its own environment.

## herdr config, and what Muster writes into it

`[ui.sidebar.agents] rows` takes multi-row layouts. Builtins: `state_icon`,
`state_text`, `workspace`, `tab`, `pane`, `agent`, `terminal_title`,
`terminal_title_stripped`. Custom metadata tokens are `$name`. Per-token style
is documented as inline `{ token = "workspace", fg = "#89b4fa", bold = true }`.
**Answered 2026-09-06: there is no global style table.** A
`[[ui.sidebar.agents.token_styles]]` block is rejected with `unknown config key
ui.sidebar.agents.token_styles; ignoring key`. Style has to be inline in the row,
and a custom token keeps its `$` inside the inline form:
`{ token = "$repo_tag", fg = "#83a598", bold = true }` validates, while
`{ token = "repo_tag", ... }` fails with `custom tokens must start with '$'`.

To check config without touching the real one, redirect the config home:
`XDG_CONFIG_HOME=/tmp/fake herdr config check` reads
`/tmp/fake/herdr/config.toml`. `herdr config check` itself takes no path.

`[[keys.command]]` accepts `type = "shell" | "popup" | "plugin_action"`.

**Answered 2026-09-06: `plugin_action` cannot open a `[[panes]]` entrypoint.**
Only `[[actions]]` ids are addressable. `plugin action list` returns the
`[[actions]]` entries and not the `home` pane entrypoint, and
`herdr plugin action invoke muster.home` returns `plugin_action_not_found` while
`muster.open` runs. So `prefix+m` binds an `[[actions]]` entry that asks the
server to open the pane, over the socket rather than through the herdr binary
(`plugin.pane.open`, see internal/ui/run.go), costing one extra process spawn on
the hottest path.

Watch out: `herdr config check` does not resolve action ids. It accepts
`command = "totally.bogus-nonexistent"` as `config: ok`, so a keybinding
pointing at a pane entrypoint validates cleanly and then does nothing when
pressed.

`[ui.toast] delivery` defaults to `off`, so notifications need opting in.

## Tab bar entries

`[ui] tab_bar_right` entries take no style and no click. A `text` entry accepts
only `text`, a `command` entry only `command`, `interval_seconds` and
`timeout_seconds`. Anything else is a parse error, and on a parse error herdr
uses defaults for the whole config.

**Answered 2026-09-13: a `command` entry runs through `/bin/sh -c` on the
server**, in the server's cwd, with the server's `PATH` and no `HERDR_PLUGIN_*`
env. A headless `herdr --session <name> server` on a throwaway
`XDG_CONFIG_HOME` runs them with no client attached, which is how this was
checked. So Muster's entry carries absolute, single-quoted paths and
`--state-dir`.

`[[keys.command]]` accepts `description`. `label`, `title`, `name`, `help`,
`desc` and `summary` are all unknown keys. **Confirmed 2026-09-13:** the
`prefix+?` help shows the description under the key, where a binding without
one reads "custom command". The badge renders in a live tab bar too.
