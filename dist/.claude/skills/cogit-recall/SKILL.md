---
name: cogit-recall
description: Read the shared context before choosing an approach, and resync when a write was denied. Use before picking a library, a pattern, a convention or an architecture — "should we…", "how do we…", "what's our rule on…" — and whenever an Edit or Write is refused with a `cogit` message. Reads `refs/heads/context`; changes no code.
---

# Recalling the shared context

The rules the team already decided are injected at session start — but only the
invariants, capped. The rest is reachable on demand, and **nothing is ever
unreachable**: search covers 100% of the corpus, including whatever was truncated
out of the injected block.

## Running `cogit`

`cogit` is a static binary committed under `.claude/cogit/bin/`, one per platform. The
project needs no runtime of its own — Node, Java, Python, it does not matter. Invoke
it through the shim for your platform:

```sh
.claude/cogit/cogit.sh <command>       # macOS, Linux
.claude\cogit\cogit.cmd <command>       # Windows
```

Written `cogit <command>` below for brevity.

## Before deciding anything

```sh
cogit find "pagination erreurs api"   # ranked, ~8 lines
cogit show <id>                        # one entry in full
cogit list decisions|facts|notes
```

Check before choosing a library, a pattern or a convention. A colleague may have
settled the question last week on another machine, and re-deriving it differently
is the exact cost this branch exists to remove.

**A rule found here binds you.** If your plan contradicts one, say so and stop —
raise it with the user rather than quietly deviating. If the rule is wrong, the
answer is a new decision that supersedes it, not an exception in the code.

## When a write is denied

A refusal means a teammate published a decision or a fact after this session
started, and you have not read it.

```sh
cogit sync
```

It fetches, prints **only the delta** — a few lines, not the corpus — re-pins the
session and unblocks Edit/Write. Then read what it printed before resuming: the
point was never the ceremony, it was those two or three lines.

Offline, or if cogit is broken: `cogit sync --offline` re-pins locally and
unblocks immediately. Use it rather than fighting the guard.

**Notes and journal entries never cause a refusal.** If you are blocked, a real
rule changed.

## Diagnosing

```sh
cogit doctor             # corpus health, brief size against its cap
cogit debug on       # trace every hook decision to .claude/cache/cogit/cogit.log
cogit debug tail
```

`doctor` is the one to run when the injected block starts feeling heavy: it reports
the brief's byte size against its cap, expired facts, dangling `supersedes`, and
duplicate identifiers.

## What not to do

- **Never read `.cogit/` by globbing the whole tree** to answer a question. That is
  what `find` is for, and it returns eight lines instead of the corpus.
- **Never edit anything under `.cogit/` directly.** It is a git worktree of the
  shared branch; use `cogit-record`.
- **Never disable the guard** (`.claude/cache/cogit/DISABLED`) to get a write
  through. Sync instead — it takes one command and it is the entire point.
