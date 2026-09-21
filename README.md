# Muster

A [herdr](https://herdr.dev) plugin. One overlay, one keystroke, showing every
agent across every repo you have work in.

herdr's sidebar handles the straightforward case well. One workspace, one
agent, everything visible. That breaks down with many agents across many
repos, especially with an orchestrator handing work between them. Two flat
lists, workspaces here and agents there, with little visible state. No sorting
or searching, and no single place to land and see the whole picture.

Muster is that landing place. One tile per agent, and a dim one for each
workspace with no agent in it. Each agent tile shows what that agent is doing
and how long it has sat quiet, and marks a task line the agent has since moved
on from as stale. Blocked ones show the question they are asking. Finished ones
stay visible until seen, and ones that depend on another repo say which one has to land first. The
ribbon on top pulls forward the ones that need attention now. Sort, search,
rearrange, and jump straight to the pick. The strip along the bottom keeps the
orchestrator and its last message in view, with keys to talk to it directly.

Task lines come from the reporting skill. It asks the orchestrator to write
down what each agent is doing at dispatch time, so the overlay shows real work
instead of guessing from terminal titles.

![The Muster overlay: a ribbon of the agents that need you, above one tile per
agent showing its status, age, task line and what it waits on, with the
orchestrator's last message along the bottom, beside herdr's own
sidebar](docs/overlay.png)

## Install

Needs herdr 0.8.2 or newer and Go on `PATH`; herdr builds plugins from source. Full list
under [Requirements](#requirements).

```sh
herdr plugin install ofelcan164/muster
```

That is the whole install. It tracks the default branch, and `--ref v0.2.0`
pins a release instead. Muster binds its keys itself: at every start herdr runs its
startup hook, `./bin/muster install --auto` from the plugin's checkout, which
starts the daemon and writes the keybindings.
Installing mid-session gets no startup hook, so opening the overlay once from
herdr's action menu does the same job.

To keep the overlay and skip the global keys, use the **Remove Muster's
keybindings, keep the overlay** action. It records the refusal in the state
dir, so the startup hook stops putting them back. The **Install Muster's
keybindings** action asks for them again. (The **Uninstall Muster's keybindings
and skill** action is the full removal, skill included.)

Everything you do with an installed Muster goes through herdr: the keys,
herdr's action menu, or `herdr plugin action invoke muster.<action>` from a
shell (see [Actions](#actions)). `herdr plugin install` puts the binaries in
herdr's own checkout rather than on your `PATH`, and only herdr hands them the
state dir and socket they need, so there is no `muster` command to type.

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
`chain.json` when the orchestrator first records a usual order.

Install also writes the reporting skill, into your agent directories rather
than herdr's. It puts one copy in `~/.agents/skills/muster-report/` and
symlinks it into every runtime you actually have: `~/.claude/skills`,
`~/.codex/skills` (`CODEX_HOME` honoured) and `~/.config/opencode/skill`. A
runtime you have not installed is left alone rather than created. The copy
names the absolute path to `muster` and its state dir, which an agent pane has
no other way to find, so every install rewrites it. The **Uninstall Muster's
keybindings and skill** action removes it and records the refusal, so the
startup hook stops putting it back, and **Install the reporting skill for the
orchestrator** asks for it again. No action removes the skill on its own.

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
next free letter out of `m g u y`, saying which one. To pick the letter
yourself, change it in `key = "prefix+m"` inside Muster's marked block in your
herdr config, then run the **Install Muster's keybindings** action. Install
reads that letter back, rewrites the other two bindings and the badge to match,
and the startup hook keeps it from then on. If the letter you picked is already
bound, install moves to the next free one instead. Muster never installs
without a key, because the overlay would then only be reachable from herdr's
action menu.

In the overlay:

- arrows or `hjkl` move, `/` searches, `enter` jumps to the tile's agent or
  focuses an empty tile's workspace
- `esc` leaves search, then clears the filter, then folds the orchestrator's
  message, then closes; `q` or `ctrl+c` closes
- `s` cycles sort (first seen, a-z, attention, herdr); `J`/`K` move the
  selected tile's workspace one place in herdr's own order, and only do
  anything in herdr sort; `g`/`G` jump to the ends
- digits `1`-`9` jump to a ribbon row, `x` dismisses one until its status
  changes
- `o` marks the selected agent as the orchestrator
- `i` messages the orchestrator, `t` tells it about a landed row: work landed
  and the agents that depend on it have not moved
- `e` shows everything the orchestrator last said, not just the one line the
  strip has room for, and folds it again
- `S` installs the reporting skill, but only while its banner is on screen
- `M` jumps to the orchestrator. Muster opens as a herdr popup, which gets
  every key while it is open, your prefix included, so this is what keeps
  `prefix+shift+m` working. `prefix+m` does nothing inside; close with `q` or
  `esc`. `prefix+ctrl+m` arrives as `enter` and jumps to the selected tile
- mouse: click a tile to jump to it or focus its workspace, click the banner to install the skill, click
  `e more` or `e less` on the strip to expand or fold the message, wheel
  scrolls, hover highlights. A click outside Muster does nothing: herdr keeps
  clicks outside a popup to itself

There is no `?` binding and no in-app legend, so this list is the reference.

## Actions

Everything an installed Muster offers is an action. Pick one from herdr's
action menu, or run it from a shell:

```sh
herdr plugin action invoke muster.open                # open the overlay
herdr plugin action invoke muster.jump-orchestrator   # focus the orchestrator
herdr plugin action invoke muster.back                # back to the previous agent
herdr plugin action invoke muster.install             # bind the keys, start the daemon, write the skill
herdr plugin action invoke muster.install-skill       # write the reporting skill
herdr plugin action invoke muster.uninstall-keys      # drop the keys, keep the overlay
herdr plugin action invoke muster.uninstall           # remove the keys and the skill
herdr plugin action invoke muster.doctor              # check health, restart a stuck daemon
herdr plugin action invoke muster.update              # install the newest release
```

**Mark this agent as the orchestrator** acts on a pane, so pick it from that
pane's action menu, or press `o` on the agent's tile. For a named herdr
session, put `--session <name>` straight after `herdr`.

There is nothing to configure. Muster reads no config of its own.

## Dependencies

Some work can be written in parallel but not tested or merged until work in
another repo is on main. web can build its checkout UI alongside api's new
endpoints, and still has to wait for api to land before it can test against
them.

The orchestrator records that on the waiting agent's pane:

```sh
herdr pane report-metadata w3:p1 --source muster \
  --token task="checkout UI against the new api endpoints" \
  --token depends_on="api#412" --ttl-ms 86400000
```

The part before `#` names the repo. The web tile then reads
`⧗ depends on ◆ api · can't land yet`, and each api tile reads
`▸ needed by ▣ web`.
Selecting or hovering either tile brightens the line on the other.

Muster cannot see a merge, and herdr's `done` only means an agent's turn ended.
So when you tell the orchestrator the PR merged and main is pulled, it writes
`landed="api#412"` on the same pane and the tile reads `landed 5m ago`. If it
then ends its turn without moving web on, a `LANDED` row reaches the top of
the ribbon, and `t` sends it the reminder.

The orchestrator also keeps a usual order between repos for the session, as
`chain.json` in the state dir, and asks Muster where dependent work stands. No
action does either. An agent pane has neither `muster` on its `PATH` nor the
state dir, so install writes the full command into the reporting skill, and the
orchestrator runs it from there:

```sh
<muster> chain set "contracts > api > web,mobile" --by orchestrator
<muster> show          # the order, then every agent on either end of an edge
<muster> show web      # one repo; repo/agent or a pane id also work
```

`<muster>` is that full command, as written in
`~/.agents/skills/muster-report/SKILL.md`, and it works the same from any herdr
pane. `>` is sequence, `,` is parallel. The order is a default the orchestrator
reads before it writes `depends_on`, and Muster draws nothing from it: an order
that holds for one feature can reverse for the next.

The skill also teaches the orchestrator to write the task lines at dispatch
time and rewrite them when the work changes. Without them Muster falls back to
terminal titles, then branch and directory, labelled as guesses.

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

Each herdr session gets a daemon, snapshot, sort and usual order of its own.
herdr hands every session the same plugin state dir, so a named session keeps
its files in `sessions/<name>/` inside it, and the default session keeps the
top level.

Colour is a pure hash of the repo key, so it matches on any machine. Sigils are
not: they follow the grid slot, skipping any mark already on screen, so two
repos on screen at once never carry the same one. Grid slots are pinned in
`state.json` on first sight. First-seen sort orders tiles by them, and a repo
keeps its sigil while the set of repos on screen stays the same. No sigil is
round or shaped like a status icon, so a repo's mark never reads as a state.

`docs/herdr-api-notes.md` has the verified herdr mechanics the daemon is built
on, including why events are a hint and never a log.

## Updating

Run the **Update Muster** action. It looks up the newest `vX.Y.Z` tag on
GitHub, and if that is newer than the installed version it reinstalls Muster at
that tag and rewrites the keys and reporting skill for the new build. The daemon
notices the new binary within seconds and restarts itself; an open overlay keeps
the old build until you reopen it. The result is in `herdr plugin log list`.
A linked checkout is refused: pull and rebuild it yourself.

By hand, installing again is the update:

```sh
herdr plugin install ofelcan164/muster --yes
```

That pulls the default branch; `--ref <ref>` pins something else. It runs no
hook, so run the **Install Muster's keybindings** action, or restart herdr,
to rewrite the reporting skill for the new build.

## Health check

Run the **Check Muster's health** action when the overlay looks wrong and
reopening it does not help: no tiles while agents are running, tiles that stop
changing, or an update that never showed up.

The usual cause is a daemon that no longer matches the install. It restarts
itself when an update rebuilds its binary, but not when the checkout it started
from has been deleted, and a daemon that stops writing its snapshot still holds
the lock, so nothing starts a new one. The check looks at the daemon, the
snapshot, the keybindings and the reporting skill, and restarts a daemon in
either state. It never touches the state dir or your config: for missing keys
or a missing skill it names the command to run instead.

From a herdr pane in a source checkout, `./bin/muster doctor` runs the same
check and asks before restarting anything.

## Uninstall

```sh
herdr plugin action invoke muster.uninstall   # removes the key block and the skill, leaves state
herdr plugin uninstall muster                 # herdr plugin unlink muster, for a source checkout
```

The uninstall action removes the marked block from your herdr config (backup
beside it), removes `~/.agents/skills/muster-report/` along with every runtime
symlink into it, and reloads the config. It also records the refusal, so the
startup hook does not rebind the keys or rewrite the skill at the next herdr
start.

No action deletes the learned state. Once the plugin is gone and herdr has
restarted, so no daemon is left to write it back, remove the state dir if it is
still there: `~/.local/state/herdr/plugins/muster`, or the same path under
`$XDG_STATE_HOME`.

## Troubleshooting

- The daemon is gone after herdr was down: by design it exits after 60s of an
  unreachable server and lives and dies with herdr. Run the install action or
  the health check, and either starts it again.
- Linked the plugin and nothing happens: startup hooks do not fire on
  `plugin link` mid-session. Open the overlay once from herdr's action menu,
  which starts the daemon and binds the keys, or run the install action.
- Muster closed without landing where you picked, or flashed and closed on
  open: the error is in `musterd.log` in the state dir. The popup is gone
  before anything it prints could be read.
- Wrong or empty after a reinstall (0 workspaces with agents up, stale tiles):
  run the **Check Muster's health** action, see [Health check](#health-check).
- Tiles show terminal titles instead of task lines: the reporting skill is
  missing. Press `S` while its banner is up, or run the **Install the reporting
  skill for the orchestrator** action.
- The orchestrator's `show` or `chain` fails with "no such file" after the
  plugin moved, say from a linked checkout to an installed one: the skill still
  names the old path. Run the **Install Muster's keybindings** action to
  rewrite it.
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

No toolchain manager required, here or anywhere else. `go.mod` is the only
place a Go version is written down, and CI reads it with
`setup-go: go-version-file`.

Testing needs a running herdr server with a few agents; launching the herdr TUI
from an agent session will hang it, so drive it from the CLI. A named session
keeps that away from your real one: `herdr --session scratch server` starts one
headless, `herdr --session scratch <command>` drives it, and Muster keeps its
state for it in `sessions/scratch/`. Read it with
`HERDR_SESSION=scratch ./bin/musterd --state-dir <dir> dump`. That covers less
than it used to: the popup has no pane id, so the CLI can open and kill it but
not read it or type into it. A server with no terminal attached also cannot show
whether a jump moves what you see, which is how a jump that landed nowhere
passed every headless check.

## License

MIT. See [LICENSE](LICENSE).
