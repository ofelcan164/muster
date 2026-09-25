# Reading Muster from outside herdr

For programs other than the overlay that show Muster's state, such as the
Omarchy bar widget. Read the files; act through the `muster` CLI.

## Where

Pass the state dir with `--state-dir` in front of every command:
`~/.local/state/herdr/plugins/muster`, or `$XDG_STATE_HOME/herdr/plugins/muster`.
That's the default herdr session. A named session keeps its files in
`sessions/<name>/` inside it. herdr installs the binaries outside `PATH`; the
`muster badge` entry Muster adds to herdr's `tab_bar_right` names the absolute
path.

## snapshot.json

`musterd` rewrites it atomically every few seconds, sooner on herdr events.

`snapshot_version` is the shape described here, currently `1`. It goes up
when a field below changes meaning or goes away; new fields don't raise it.
Snapshots from before it existed have no such field and match version 1.

A snapshot whose `generated_at` is 30 s old or more means `musterd` isn't
running. Show that as stale, never as "nothing needs you".

| Field | Meaning |
|---|---|
| `snapshot_version` | This document's version |
| `generated_at` | RFC 3339 time of the write |
| `daemon_pid` | The daemon that wrote it |
| `repos[]` | One per repo: `key` (stable id), `display` (short name to draw), `sigil`, `color_index` (into the palette below), `is_git`, `agents[]`, `other_panes[]` |
| `repos[].agents[]` | `pane_id`, `name`, `status` (`idle`, `working`, `blocked`, `done`, `unknown`), `status_since` |
| `attention[]` | The ribbon, most urgent first. `reason` (`blocked`, `landed`, `process_stopped`, `done_unseen`, `idle_never_done`), `status`, `repo_key`, `pane_id`, `agent`, `detail`, `age_ns`, `age_known` |
| `orchestrator` | `found`; when true, `pane_id`, `name`, `status`, `last_said` and `said_at` |
| `counts` | `agents`, `working` |

The overlay draws at most four ribbon rows, after leaving out dismissed ones.

## ui.json

Written by the overlay and by `muster dismiss`, under `ui.json.lock`. Read it;
don't write it.

| Field | Meaning |
|---|---|
| `dismissed` | Pane id to the status its row was dismissed at. A row is hidden while its `status` still equals that value |
| `colors` | Repo key to a palette index picked with `c`. It overrides the snapshot's `color_index` |

The palette is `identity.Palette` in `internal/identity/identity.go`.

## Acting

Every command takes `--state-dir <dir>` first. They reach the default
session's herdr socket, `~/.config/herdr/herdr.sock`, unless
`HERDR_SOCKET_PATH` names another.

| Command | Does | Overlay key |
|---|---|---|
| `muster jump <pane>` | Focus that pane in herdr | `enter` |
| `muster jump orchestrator` | Focus the orchestrator | `M` |
| `muster tell <text>` | Send the orchestrator a message | `i` |
| `muster report <pane>` | Tell the orchestrator about that pane's `landed` row | `t` |
| `muster dismiss <pane>` | Hide that pane's ribbon row until its status changes | `x` |
| `muster mark-orchestrator <pane>` | Make that pane's agent the orchestrator | `o` |
| `muster badge --json` | `{text, tooltip, class}` for a status bar | |

Each exits 0 when it worked. A failure prints the reason on stderr and exits
1; a missing argument exits 2.
