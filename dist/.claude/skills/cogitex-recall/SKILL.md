---
name: cogitex-recall
description: Read the shared context before choosing an approach, and resync when a write was denied. Use before picking a library, a pattern, a convention or an architecture — "should we…", "how do we…", "what's our rule on…" — and whenever an Edit or Write is refused with a `cogitex` message. Reads `refs/heads/context`; changes no code.
---

# Recalling the shared context

The rules the team already decided are injected at session start — but only the
invariants, capped. The rest is reachable on demand, and **nothing is ever
unreachable**: search covers 100% of the corpus, including whatever was truncated
out of the injected block.

## Running `cogitex`

`cogitex` is a static binary committed under `.claude/cogitex/bin/`, one per
platform. The project needs no runtime of its own — Node, Java, Python, it does
not matter. Invoke it through the shim for your platform:

```sh
.claude/cogitex/cogitex.sh <command>        # macOS, Linux, Git Bash
.claude\cogitex\cogitex.cmd <command>       # Windows
```

Written `cogitex <command>` below for brevity.

## Before deciding anything

```sh
cogitex find "pagination erreurs api"   # ranked, ~8 lines
cogitex show <id>                        # one entry in full
cogitex list decisions|facts|notes
```

Check before choosing a library, a pattern or a convention. A colleague may have
settled the question last week on another machine, and re-deriving it differently
is the exact cost this branch exists to remove.

`find` reaches everything this session can see — including the body of a note,
which is where dead ends and their reasons live, and `rationale`, which is where
the *why* lives. It stops at eight hits and says so; `--all` lifts that.

**A rule found here binds you.** If your plan contradicts one, say so and stop —
raise it with the user rather than quietly deviating. If the rule is wrong, the
answer is a new decision that supersedes it, not an exception in the code.

**A draft does not bind anyone**, not even its author. Entries whose id starts
with `drafts/` are the current user's own unfinished thinking; the block injected
at session start labels them as such. Never cite one as a team rule, and never
enforce one on somebody else's code. Other people's drafts are invisible here —
they are not part of this session's corpus, and that is deliberate.

## When a write is denied

A refusal means a teammate published a decision or a fact after this session
started, and you have not read it.

```sh
cogitex sync
```

It fetches, prints **only the delta** — a few lines, not the corpus — re-pins the
session and unblocks Edit/Write. Then read what it printed before resuming: the
point was never the ceremony, it was those two or three lines.

Offline, or if cogitex is broken: `cogitex sync --offline` re-pins locally and
unblocks immediately. Use it rather than fighting the guard.

**Notes and journal entries never cause a refusal.** If you are blocked, a real
rule changed.

## Diagnosing

```sh
cogitex doctor             # corpus health, brief size against its cap
cogitex debug on       # trace every hook decision to .claude/cache/cogitex/cogitex.log
cogitex debug tail
```

`doctor` is the one to run when the injected block starts feeling heavy: it reports
the brief's byte size against its cap, expired facts, dangling `supersedes`, and
duplicate identifiers.

## What not to do

- **Never read `.cogitex/` by globbing the whole tree** to answer a question. That is
  what `find` is for, and it returns eight lines instead of the corpus.
- **Never edit anything under `.cogitex/` directly.** It is a git worktree of the
  shared branch; use `cogitex-record`.
- **Never disable the guard** (`.claude/cache/cogitex/DISABLED`) to get a write
  through. Sync instead — it takes one command and it is the entire point.
