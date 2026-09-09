---
name: muster-report
description: Record what a herdr agent is working on, so Muster can show it. Use right after dispatching, delegating or handing work to another agent or pane, whenever what an agent is working on changes, and when an agent is parked waiting on another repo. Also use when an agent shows no task line in Muster, or shows one that is out of date.
---

# Reporting what an agent is working on

Muster shows one line per agent saying what it is doing. That line comes from
metadata you write. Nothing else can produce it: an agent's terminal title is a
guess, and its own output is not something Muster reads.

Write it as soon as you dispatch the work, not when you get round to it. A task
line that arrives late is a line Muster spent that whole time unable to show.

## The command

```
herdr pane report-metadata <pane-id> --source muster \
  --title "short label" \
  --token task="what this agent is actually doing" \
  --ttl-ms 86400000
```

`<pane-id>` is the pane the agent runs in, as `pane list` reports it.

## The tokens Muster reads

| Token | What it is for |
|---|---|
| `task` | What this agent is doing. One sentence, present tense. This is the line Muster shows. |
| `blocked_on` | What this agent is waiting on, when it is parked. A PR reference, a repo name, whatever names the thing. |
| `note` | Anything you want on the card that is not the task. |
| `role` | Set to `orchestrator` on your own pane. Muster's mark-orchestrator action does this for you. |

Set `--ttl-ms 86400000` so a line expires after a day rather than outliving the
work by a week.

Your own pane is the one exception to the `task` token. Muster writes that one
itself: the `i` and `t` keys in the overlay record the message they just sent
you, so the strip can show what you were last told. Writing over it costs you
nothing, but it will be replaced the next time someone types into the overlay.

## Rewrite it when the work changes

Re-run the command with the new `task`. Muster times how long a value has been
in place, and dims a line that has not changed since the agent last changed
state, on the grounds that a task line which was true forty minutes ago and is
now wrong is worse than no line: you would trust it. Keeping the token current
is what keeps the line bright.

## When an agent is parked

```
herdr pane report-metadata w3:p1 --source muster \
  --token task="checkout UI, parked" \
  --token blocked_on="api#412" \
  --ttl-ms 86400000
```

## What not to do

Do not write a `task` you are not sure of. Muster falls back to the terminal
title and then to the branch and directory, and both of those are honestly
labelled as guesses. A confident wrong line is worse than either.
