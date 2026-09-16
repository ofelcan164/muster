---
name: muster-report
description: Record what a herdr agent is working on and what it waits on, so Muster can show it. Use right after dispatching, delegating or handing work to another agent or pane, whenever what an agent is working on changes, when an agent depends on work in another repo, and when the user says a PR or MR that dependent work waits on has merged. Also use to check where a body of work stands, and when an agent shows no task line in Muster or one that is out of date.
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

Each time you write to a pane, repeat every token you want it to keep. Drop one
with `--clear-token <name>`.

## The tokens Muster reads

| Token | What it is for |
|---|---|
| `task` | What this agent is doing. One sentence, present tense. This is the line Muster shows. |
| `depends_on` | Work in another repo this agent has to wait for before it can test or merge, as `<repo>#<pr>`, for example `api#412`. Name the repo the way `show` lists it. Muster draws the dependency on both agents' tiles. |
| `landed` | The same value as `depends_on`, written once that work is on main. Only an exact match counts. |
| `note` | Anything else worth recording. Read back by `show`, not shown on the card. |
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

## Asking Muster where work stands

Muster is not on your `PATH`, and your pane does not know its state directory.
This is the full command on this machine:

```
{{muster}} show                   the usual order, then every agent on either end of a dependency
{{muster}} show web               every agent in one repo
{{muster}} show web/checkout-ui   one agent; a pane id works too
```

Each agent comes back with its status and age, its task line, what it waits on
and whether that landed, which agents wait on its repo, and any row it holds at
the top of Muster's overlay. Ask this instead of reading panes when you need to
know where work stands.

## When work depends on another repo

Run `{{muster}} show` first. Its `usual order` line is the order repos normally
land in, such as `contracts > api > web,mobile`, where `>` is sequence and `,`
is parallel. Follow it unless the user says this feature goes differently.

Dispatch the dependent work anyway, since it can be written in parallel, and
record what it waits on:

```
herdr pane report-metadata w3:p1 --source muster \
  --token task="checkout UI against the new api endpoints" \
  --token depends_on="api#412" \
  --ttl-ms 86400000
```

When its code is written and it can go no further, rewrite `task` to say so,
for example `checkout UI written, waiting on api#412`, and keep `depends_on`. A
dependent agent sitting idle is normal, and Muster shows it that way.

When you and the user settle on a new usual order, record it so the next
orchestrator finds it:

```
{{muster}} chain set "contracts > api > web,mobile" --by orchestrator
```

## When the work it depends on lands

Muster cannot see a merge. The user will usually tell you that a PR or MR has
merged and main is pulled. When they do, write `landed` on every pane that depends
on it, with the value its `depends_on` already has:

```
herdr pane report-metadata w3:p1 --source muster \
  --token task="checkout UI, api#412 landed, rebasing" \
  --token depends_on="api#412" \
  --token landed="api#412" \
  --ttl-ms 86400000
```

`{{muster}} show api` lists who depends on it. Then tell each of those agents
to rebase onto main, test and merge. If you record a landing and end your turn
without moving them, Muster puts a LANDED row at the top of its overlay.

Once the dependent work has merged, drop both tokens:

```
herdr pane report-metadata w3:p1 --source muster \
  --token task="checkout UI merged" \
  --clear-token depends_on --clear-token landed \
  --ttl-ms 86400000
```

## What not to do

Do not write a `task` you are not sure of. Muster falls back to the terminal
title and then to the branch and directory, and both of those are honestly
labelled as guesses. A confident wrong line is worse than either.

Do not write `landed` because an agent finished its turn. An agent finishing
means it stopped, not that its work reached main.
