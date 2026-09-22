---
description: Record something the team has not agreed to yet — yours alone, blocking nobody
argument-hint: [what to draft]
allowed-tools: Bash
---

Draft: $ARGUMENTS

A draft is an ordinary entry written under `drafts/<you>/`. It is injected into
**your** sessions and nobody else's, it blocks no one, and it follows you from one
machine to the next.

```sh
echo '{"title":"…","type":"convention","domain":"api","scope":"global","decision":"…","rationale":"…"}' \
  | .claude/cogitex/cogitex.sh add decision --draft     # macOS, Linux, Git Bash
```

On Windows use `.claude\cogitex\cogitex.cmd`. If neither shim exists in this
project, cogitex comes from the plugin — run `cogitex add decision --draft`.

Pick the kind the same way you would for a real entry: `decision` for a standing
rule, `fact` for something that changed, `note` for an idea or a dead end. The
`--draft` flag only changes **who sees it**, never what it is — and the validation
is identical, so promoting it later is a pure move.

Then:

```sh
cogitex list drafts
cogitex promote <id>     # turns it into a team rule — this is what interrupts everyone
cogitex drop <id>        # it is yours, so deleting it is the normal move
```

Two things to say out loud if they come up: a draft **binds nobody**, so never
cite one as if the team had agreed to it; and the branch is pushed, so a draft is
private by tooling, not by mechanism — no secrets, ever.
