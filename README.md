# Muster

**One screen for every agent across every repo.** A [herdr](https://herdr.dev)
plugin.

Press `prefix+m` and see every agent you have running: what each one is doing,
which ones are waiting on you, and which ones finished while you weren't
looking. Pick one and press `enter` to jump straight to it.

![The Muster overlay: a ribbon of the agents that need you, above one tile per
agent showing its status, age, task line and what it waits on, with the
orchestrator's last message along the bottom, beside herdr's own
sidebar](docs/overlay.png)

## Why use it

herdr's sidebar can't tell you at a glance which agents matter right now.
Muster can:

- **You lose track of who needs you.** Muster's top ribbon lists the agents
  waiting on you, blocked ones with the question they're asking.
- **Finished work goes unnoticed.** An agent that finished stays in view until
  you've looked at it.
- **Terminal titles don't say much.** With the reporting skill, each tile shows
  what the agent was actually asked to do.
- **Cross-repo work gets tangled.** If `web` is waiting on a change in `api`,
  both tiles say so, and Muster reminds you once `api` has landed.
- **Orchestrator setups are hard to follow.** The orchestrator's latest message
  stays pinned along the bottom, and you can message it without leaving Muster.

## Install

```sh
herdr plugin install ofelcan164/muster
```

Muster sets itself up and binds its keys the next time herdr starts. If you
installed it mid-session, run the **Open Muster** action once instead.

**Requirements:** herdr 0.8.2 or newer, Go 1.21+ on your `PATH`, Linux or
macOS. The reporting skill needs Claude Code, Codex or OpenCode.

## Getting started

1. **Open Muster** with `prefix+m`. There is one tile per agent, plus a dim
   tile for each workspace with no agent.
2. **Move** with the arrow keys or `hjkl`, then **jump** to an agent with
   `enter`.
3. **If you use an orchestrator**, select its tile and press `o`. From then on
   `prefix+shift+m` jumps to it, and its last message shows along the bottom of
   Muster.

Muster also puts a small status line in herdr's tab bar, for example
`◆ 3 need you · prefix+m`.

## Keys

Anywhere in herdr:

| Key              | Does                             |
| ---------------- | -------------------------------- |
| `prefix+m`       | Open Muster                      |
| `prefix+shift+m` | Jump to the orchestrator         |
| `prefix+ctrl+m`  | Go back to the previous agent    |

If `prefix+m` is already taken, Muster uses the next free letter out of
`g`, `u`, `y` and tells you which one it picked. It never overrides your own
bindings.

Inside Muster:

| Key              | Does                                                      |
| ---------------- | --------------------------------------------------------- |
| arrows / `hjkl`  | Move                                                      |
| `enter`          | Jump to the agent, or focus the empty workspace           |
| `/`              | Search                                                    |
| `s`              | Change sort: first seen, a–z, needs attention, herdr's order |
| `1`–`9`          | Jump to a row in the attention ribbon                     |
| `x`              | Dismiss a ribbon row until its status changes             |
| `o`              | Mark the selected agent as the orchestrator               |
| `M`              | Jump to the orchestrator                                  |
| `i`              | Send the orchestrator a message                           |
| `e`              | Show the orchestrator's full last message, or collapse it |
| `c`              | Give the selected repo a different colour                 |
| `g` / `G`        | First / last tile                                         |
| `J` / `K`        | Move a workspace up or down (in herdr's order sort only)  |
| `esc`            | Leave search, clear the filter, collapse the message, or close |
| `q` / `ctrl+c`   | Close                                                     |

Clicking a tile jumps to it.

## Working with an orchestrator

Muster works without any setup, but it's most useful when an orchestrator agent
hands work out to the others.

**The reporting skill** is installed automatically into Claude Code, Codex and
OpenCode, wherever you have them. It teaches the orchestrator to write down what
each agent is working on when it hands out the task, so tiles show real task
lines rather than guesses from terminal titles. If a banner in Muster says the
skill is missing, press `S`.

**Dependencies between repos.** Sometimes one agent can't finish until another
repo's work is merged, for example a web UI that needs a new API endpoint. The
orchestrator records that, and Muster shows it on both tiles:

```
web  ⧗ depends on ◆ api · can't land yet
api  ▸ needed by ▣ web
```

When you tell the orchestrator the API change has merged, the web tile shows
`landed 5m ago`. If the orchestrator then finishes its turn without getting web
moving again, a
`LANDED` row appears at the top of Muster, and `t` sends the orchestrator a
reminder.

## Actions

- **Open Muster**, **Jump to orchestrator**, **Back to previous agent**
- **Mark this agent as the orchestrator**
- **Update Muster**: installs the newest release
- **Check Muster's health**: run this if Muster looks wrong (see below)
- **Install Muster's keybindings** / **Remove Muster's keybindings, keep the
  overlay**
- **Install the reporting skill for the orchestrator**
- **Uninstall Muster's keybindings and skill**

There is nothing to configure.

## When something looks wrong

Run **Check Muster's health**. It finds and restarts a stuck or outdated
background process, and tells you if the keybindings or skill are missing.

- **Muster is empty or stops updating**, often after an update: run the health
  check.
- **Tiles show terminal titles instead of tasks**: the reporting skill is
  missing. Press `S` in Muster.
- **A key does nothing**: close and reopen Muster; an already-open Muster keeps
  running the old version after an update.
- **Muster flashes and closes**: the error is in `musterd.log` in
  `~/.local/state/herdr/plugins/muster/`.

## What it changes on your system

- Adds a clearly marked block of keybindings to your herdr config, and saves a
  backup of the config first.
- Adds the status entry to the tab bar in your herdr config.
- Installs the reporting skill in `~/.agents/skills/muster-report/`, linked
  into the agent tools you have.
- Runs a small background process, `musterd`, that follows herdr and exits when
  herdr does.

## Uninstall

Run **Uninstall Muster's keybindings and skill** before
`herdr plugin uninstall muster`: removing the plugin alone leaves Muster's
keybindings in your herdr config and the skill in your agent tools. Muster's
saved state (sort order, colours) stays in
`~/.local/state/herdr/plugins/muster`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for building from source, running the
tests and how Muster works inside.

## License

MIT. See [LICENSE](LICENSE).
