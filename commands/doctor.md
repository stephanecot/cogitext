---
description: Check the shared context — corpus health, brief size against its cap, platform
allowed-tools: Bash
---

```sh
.claude/cogitex/cogitex.sh doctor        # macOS, Linux, Git Bash
.claude\cogitex\cogitex.cmd doctor       # Windows
```

It reports the platform and git version, the injected brief's byte size against
its cap, expired facts, dangling `supersedes`, duplicate identifiers, and commits
on the context branch that were never published — teammates cannot see those
until `cogitex push`.

Summarize what it printed. Propose a fix only for what it actually flagged — and
never repair the corpus by editing `.cogitex/` by hand: entries are superseded
through `cogitex add`, never rewritten.
