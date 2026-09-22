# cogitex — shared context across users

`cogitex` carries the team's decisions, facts, notes and journal on the orphan
branch `refs/heads/context`, and wires three hooks — in Claude Code and in GitHub
Copilot alike — that inject them, refresh them, and pause a write made under
rules that were never read.

## Why a binary

This folder has to be droppable into **any project** — Java, Python, Go, .NET —
without imposing a runtime on it. `cogitex` is therefore a static binary, one per
platform, with no shared dependency whatsoever. The only thing it requires is
`git`, which is consistent: the whole system is built on it.

## Getting started, in a fresh clone

```sh
.claude/cogitex/cogitex.sh init        # macOS, Linux, Git Bash
.claude\cogitex\cogitex.cmd init       # Windows
```

`init` creates or joins the branch, mounts the `.cogitex` worktree, and writes
`.claude/settings.local.json` — **local and gitignored**, because a
`settings.json` cannot point at a different binary path per OS. That is the
accepted price of committed binaries: one command after cloning, but no
installation and no toolchain.

The GitHub Copilot hooks live in `.github/hooks/cogitex.json`: they depend on no
absolute path, so they are committed once for the whole team — and that is also
the only place Copilot's cloud agent reads. Nothing to re-run on that side after
a clone.

## Contents

```
bin/cogitex-<os>-<arch>    the binaries, ~3.5 MB each
cogitex.sh / cogitex.cmd   pick the binary for the current platform
                           (cogitex.sh also covers Git Bash and MSYS2)
```

The binaries are produced by the **cogitext** repository, where the sources and
the build script live. They are rebuilt there **only when cutting a release**:
every run adds one blob per platform to history, and git handles a frequently
changing binary badly. So this folder is never edited by hand — it is replaced,
from cogitext, with its installer.
