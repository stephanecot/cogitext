---
name: cogitex-record
description: Record a decision, a volatile fact, or a note into the shared context branch so the other developers' sessions get it. Use when the user states a standing rule or convention — "on fait toujours", "à partir de maintenant", "la règle c'est", "désormais", "from now on", "we always" — when a change breaks other people's assumptions (an endpoint signature, a schema, a removed module), or when something is parked for later. Writes to `refs/heads/context`, never to application code.
---

# Recording into the shared context

`refs/heads/context` is an orphan branch carrying what the team has decided. It is
shared across machines and users, and it is the only thing that stops two sessions
from re-deriving the same architecture in contradictory ways.

## The one rule

**Record, then go back to what you were doing.** Recording is bookkeeping, not the
task. Write the entry, say one line, continue. Do not start implementing the rule
you just recorded, and do not go auditing the codebase for violations of it unless
the user asked.

## Choosing the kind — this is the whole decision

The kinds differ on one axis that matters more than their content: **who they
freeze**. A decision or a fact makes every other running session stale and blocks
its writes until it syncs. A note blocks nobody.

- **`decision`** — a standing rule. "Identifiers are slugs." "Errors follow RFC
  7807." Permanent, superseded rather than edited. Use `scope: global` only for
  what must be true in *every* session; that budget is capped at a few dozen lines
  for everyone, forever.
- **`fact`** — something that changed and breaks other people's assumptions. "The
  dossiers endpoint returns a paginated envelope now." Volatile, expires on its own
  (`ttl_days`, 30 by default). Set `affects` to the globs it touches.
- **`note`** — an idea, a backlog item, an annoyance, and above all **a dead end
  with its reason**. "Tried a compound index, it fails because the field is
  optional." That last one is the knowledge that exists nowhere else: not in the
  code, not in git history, not in any PR — and it is the most expensive to
  rediscover.

**When in doubt, write a note.** A note costs nobody anything. A decision recorded
carelessly stops three colleagues from writing until they read it.

## Not settled yet? Draft it

`--draft` on any of the three writes the entry under `drafts/<you>/` instead. It
lands in **your** session's context and nobody else's, it never blocks anyone, and
it follows you from machine to machine.

```sh
echo '{"title":"…","type":"convention","domain":"api","decision":"…","rationale":"…"}' \
  | cogitex add decision --draft
cogitex list drafts
cogitex promote <id>      # makes it a team rule — this is what interrupts everyone
cogitex drop <id>         # yours, so deleting it is the normal move
```

Use it when the user is **thinking out loud** rather than stating a rule — "we
should probably…", "I'd like us to…", "let's try…". Recording that as a decision
would freeze three colleagues on an opinion nobody has agreed to; ignoring it
loses it. A draft is the honest third answer.

A draft passes the **same validation** as the real thing — the 110-character cap,
English, required fields — so promoting it is a pure move, and you never discover
three weeks later that it cannot be promoted.

Two limits worth stating plainly:

- **Private by tooling, not by mechanism.** The branch is pushed, so anyone who
  opens the worktree can read `.cogitex/drafts/<someone-else>/`. Never put in a
  draft what you would not put in a decision. No secrets, ever.
- **A draft binds nobody, including you.** Never cite one to the user as if the
  team had agreed to it, and never enforce one on somebody else's code.

## Running `cogitex`

`cogitex` is a static binary committed under `.claude/cogitex/bin/`, one per
platform. The project needs no runtime of its own — Node, Java, Python, it does
not matter. Invoke it through the shim for your platform:

```sh
.claude/cogitex/cogitex.sh <command>        # macOS, Linux, Git Bash
.claude\cogitex\cogitex.cmd <command>       # Windows
```

Written `cogitex <command>` below for brevity.

## One rule goes in directly; a document goes to the curator

For a single rule the user just stated, record it yourself — a subagent round trip
costs more than the entry.

**Delegate to the `cogitex-curator` agent** when the material is a document, a page
of notes, or anything yielding more than two entries. Judging duplicates means
reading candidate entries in full, and that reading has no business in this
session's context. The curator also batches the writes so the team is interrupted
once instead of once per entry.

## Writing one

```sh
echo '{"title":"…","type":"convention","domain":"api","scope":"global","decision":"…","rationale":"…","tags":["…"]}' \
  | cogitex add decision
```

`decision` is **the rule in one sentence, 110 characters maximum** — the writer is
refused beyond that, on purpose. It is the line every teammate loads at every
session start; the reasoning goes in `rationale`, which nobody pays for until they
ask.

**Every entry is written in English** — title, decision, rationale, body, tags. This
is not a style preference: the index is searched by keyword and read by sessions
whose language is not guaranteed, and a half-translated corpus breaks `find`,
since one half stops answering the other half's keywords. `cogitex add` refuses
French prose at write time, so you find out immediately rather than six months
later.

Facts and notes take `add fact` / `add note` with the same shape (`affects` and
`ttl_days` for a fact; `body` for a note).

## What you must not do

- **Never edit an existing decision to correct it.** Write a new one with
  `"supersedes": "<its id>"`. The history of the reasoning has value; overwriting
  destroys it, and other sessions have already recorded the old id.
- **Never record a rule the user did not state.** Inferring a convention from the
  code and freezing everyone's writes on it is exactly the failure this branch is
  meant to prevent.
- **Never put a secret in an entry.** The branch is pushed; GitHub's push
  protection will reject it, and the secret is already in local history by then.
- **Never `git add -A` inside `.cogitex`,** and never commit there by hand —
  `cogitex add` stages the exact two paths it wrote.

Publication is automatic and best-effort: if the remote is unreachable the commit
stays local and goes out later. Say so and move on; never block on the network.
