---
description: Search the whole shared context corpus by keyword
argument-hint: <keywords>
allowed-tools: Bash
---

Search the shared context for: $ARGUMENTS

```sh
.claude/cogitex/cogitex.sh find "$ARGUMENTS"        # macOS, Linux, Git Bash
.claude\cogitex\cogitex.cmd find "$ARGUMENTS"       # Windows
```

`find` covers 100% of the corpus — including entries truncated out of the block
injected at session start — and returns about eight ranked lines. Use
`cogitex show <id>` for any entry worth reading in full.

Report what it found, and say plainly whether a rule binds the current task. A
rule found here binds you: if the plan contradicts one, stop and raise it rather
than deviating quietly.
