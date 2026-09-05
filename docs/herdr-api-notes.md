# herdr 0.8.2 notes

Verified against the installed binary and a live server on 2026-09-05. Design
lives in `plan.html`; this file is only the mechanics needed to write code.

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

4. **Startup hooks do not fire on `plugin link` mid-session.** They fire after
   session restore on server start, and again on `herdr update --handoff`.
   So the daemon must be startable by the install action too, and `--ensure`
   must be idempotent under a lock.

## Overlay panes

Confirmed by spike:

- Get a real TTY. `stdin` is a char device.
- Get an initial size and live resizes. Bubbletea saw `86x33 -> 76x23 -> 47x21`
  while the window was dragged.
- **Do** get `HERDR_PANE_ID`. The docs only rule that out for popups.
- Appear in `pane list` with the manifest's `title` as `label`, which is how to
  find your own pane without the env var.
- Sized to the tab area, i.e. terminal width minus the sidebar.

## Jumping out of an overlay

`agent focus <target>` then exit the process. That is the whole thing.

- ~2ms, works across workspaces.
- The overlay's own focus restore does **not** steal focus back.
- No `plugin pane close` needed. No detached helper. No delay.
- Overlay leaves no residue in `pane list`.

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
```

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

## Agent state

`idle` `working` `blocked` `done` `unknown`.

`done` is idle after work you have not seen. Focusing marks seen.
**CLI reads do not mark seen**, so a daemon can poll `pane read` and
`agent read` continuously without consuming the unseen signal.

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

## Config surface Muster writes

`[ui.sidebar.agents] rows` takes multi-row layouts. Builtins: `state_icon`,
`state_text`, `workspace`, `tab`, `pane`, `agent`, `terminal_title`,
`terminal_title_stripped`. Custom metadata tokens are `$name`. Per-token style
is documented as inline `{ token = "workspace", fg = "#89b4fa", bold = true }`.
**Unverified:** whether a global per-token style table also works.

`[[keys.command]]` accepts `type = "shell" | "popup" | "plugin_action"`.
**Unverified:** whether `plugin_action` can open a `[[panes]]` entrypoint
directly or must go through an `[[actions]]` entry that shells out.

`[ui.toast] delivery` defaults to `off`, so notifications need opting in.
