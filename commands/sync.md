---
description: Fetch the shared context branch, print only what changed, and unblock writes
allowed-tools: Bash
---

Run the cogitex shim for this platform, from the project root:

```sh
.claude/cogitex/cogitex.sh sync        # macOS, Linux, Git Bash
.claude\cogitex\cogitex.cmd sync       # Windows
```

If neither shim exists in this project, cogitex comes from the plugin instead —
run `cogitex sync`, or the binary at `$CLAUDE_PLUGIN_ROOT/dist/.claude/cogitex/`.

Add `--offline` if the remote is unreachable: it re-pins this session locally and
unblocks Edit/Write right away.

Then **read the delta it printed** before doing anything else. It is a few lines,
and it is the whole point of the command: those lines are rules a teammate landed
after this session started. Say in one sentence what changed, then resume.
