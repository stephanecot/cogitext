---
applyTo: "**"
---

# Shared context (cogitex)

This repository carries the team's **decisions, facts and notes** on the orphan
branch `refs/heads/context`, reachable through the `cogitex` binary committed
under `.claude/cogitex/`. It needs no runtime — only `git`.

```sh
.claude/cogitex/cogitex.sh <command>        # macOS, Linux, Git Bash
.claude\cogitex\cogitex.cmd <command>       # Windows
```

## Before choosing a library, a pattern or a convention

```sh
cogitex find "pagination api errors"   # ranked, ~8 lines, covers 100% of the corpus
cogitex show <id>                       # one entry in full
cogitex list decisions|facts|notes
```

A colleague may have settled the question last week, on another machine. **A rule
found here binds you**: if the plan contradicts one, say so and stop rather than
deviating quietly. If the rule is wrong, the answer is a new decision that
supersedes it, not an exception in the code.

## When a write is refused with a `cogitex:` message

A teammate published a decision or a fact after this session started.

```sh
cogitex sync          # prints only the delta, re-pins the session, unblocks writes
cogitex sync --offline    # same, locally, when the remote is unreachable
```

Read the few lines it prints before resuming. Notes never cause a refusal — if
you are blocked, a real rule changed.

## When the user states a standing rule

"from now on", "we always", "the rule is" — record it, then go back to the task:

```sh
echo '{"title":"…","type":"convention","domain":"api","scope":"global","decision":"…","rationale":"…"}' \
  | cogitex add decision
```

`decision` is the rule in one sentence, 110 characters maximum. Every entry is
written in **English**, whatever the language of the conversation: the corpus is
searched by keyword, and a half-translated corpus breaks `find`.

Use `add fact` for something that changed and breaks other people's assumptions
(volatile, expires on its own), and `add note` for an idea or a dead end with its
reason. **When in doubt, write a note**: a note blocks nobody, a decision pauses
every teammate's writes until they read it.

## When the user is thinking out loud

"we should probably…", "let's try…" — that is not a rule yet. Add `--draft` and it
goes under `drafts/<them>/`: injected into their own sessions, invisible to
everyone else, blocking nobody, and following them across machines.

```sh
cogitex add decision --draft     # same JSON, same validation
cogitex list drafts
cogitex promote <id>             # THIS is what makes it binding for the team
```

An entry whose id starts with `drafts/` binds nobody, not even its author. Never
cite one as a team rule. And never put a secret in one: the branch is pushed, so a
draft is private by tooling, not by mechanism.

Never record a rule the user did not state, never edit `.cogitex/` by hand, and
never put a secret in an entry — the branch is pushed.
