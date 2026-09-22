# cogitex — shared context across sessions

`cogitex` carries a team's **decisions, facts, notes and journal** on an orphan
git branch, `refs/heads/context`, and wires three hooks — in **Claude Code** and
in **GitHub Copilot** alike — that inject them at session start, refresh them
mid-session, and pause a write made under rules the session has not read.

The context therefore travels with the repository: no service, no database, no
account. What one session knows, the next one knows too — including on someone
else's machine, and including in the other agent.

## Why a binary

This has to install into **any project** — Java, Python, Go, .NET, TypeScript —
without imposing a runtime on it. `cogitex` is therefore a static binary, one per
platform, with no shared dependency whatsoever. The only thing it requires is
`git`, which is consistent: the whole system is built on it.

That is the reason for this separate repository: the code used to live inside an
application project, which had no business carrying everyone else's tooling.

## Two ways to install it

| | As a plugin | Dropped into the project |
|---|---|---|
| Scope | all your projects, on your machine | this project, for the whole team |
| Installed by | `/plugin install` | `install.sh` / `install.ps1` |
| Who benefits | you | everyone, plus Copilot's cloud agent |

Both can coexist: when a project wires its own hooks, the plugin stands down —
otherwise every turn would pay for the same hook twice.

### As a plugin

This repository is both a plugin and its own marketplace, for both agents.

```sh
# Claude Code
/plugin marketplace add stephanecot/cogitext
/plugin install cogitex@cogitex

# GitHub Copilot CLI
copilot plugin marketplace add stephanecot/cogitext
copilot plugin install cogitex@cogitex
```

The plugin brings the hooks, both skills, the curator agent and the `/sync`,
`/find`, `/doctor` and `/draft` commands. Each project still has to be bootstrapped
once, with `init`.

### Dropped into the project

```sh
./install.sh /path/to/the/project            # macOS, Linux, Git Bash
.\install.ps1 C:\path\to\the\project         # Windows
```

The installer copies `dist/` into the project, then adds to its `.gitignore` and
`.gitattributes` the lines without which cogitex misbehaves — without the second
one, the binaries are corrupted by line-ending conversion on a Windows clone.
Nothing is overwritten silently: a file already present and different is reported
and kept, unless `--force` / `-Force` is given.

Then, inside the project:

```sh
.claude/cogitex/cogitex.sh init        # macOS, Linux, Git Bash
.claude\cogitex\cogitex.cmd init       # Windows
```

`init` creates or joins the branch, mounts the `.cogitex` worktree, and merges
the Claude Code hooks into `.claude/settings.json` — keeping everything else that
file holds, and removing the per-machine hooks an older `init` wrote into
`settings.local.json`.

Both agents' hooks are **the same on Windows, macOS and Linux**, so both are
meant to be committed. **Commit `.claude/settings.json` and
`.github/hooks/cogitex.json`** — that is what gives them to the team, and to
Copilot's cloud agent, which reads nothing but `.github/hooks/`.

Everyone who clones the project still runs `init` once, to mount the worktree;
the hooks are already there, and stay silent until the branch is.

### How one hook command works on three systems

A Claude Code hook either goes through a shell — `sh` on macOS and Linux, Git
Bash on Windows, **PowerShell when Git Bash is missing** — or, when it has
`args`, spawns its executable directly. The binary's name differs per OS, and
PowerShell cannot run a `.sh`: no single shell command covers the three.

So the hooks run **`git`**, in exec form — the one executable that exists under
that name everywhere, and that cogitex needs anyway — with an alias:

```json
{ "command": "git",
  "args": ["-c", "alias.cogitex=!.claude/cogitex/cogitex.sh", "cogitex", "hook-guard", "--claude", "${CLAUDE_PROJECT_DIR}"] }
```

git runs a `!` alias with the `sh` it ships — Git for Windows included — from
the repository's top level, and the launcher picks the binary for the platform.
stdin, stdout and the exit code pass through untouched. Copilot needs no trick:
its hook format has a `bash` and a `powershell` field.

## Commands

```
init [--no-hooks]       create or join the branch, mount .cogitex, wire the hooks
add decision|fact|note  record an entry (JSON on stdin) [--draft] [--no-push]
promote <id>            turn a draft into a team entry (moves the gate)
drop <id>               delete one of your own drafts
push                    publish local commits in one move
sync [--offline]        fetch, print the delta, re-pin the session
find "<words>" [--all]  search the whole corpus
show <id>               print one entry in full
list decisions|facts|notes|drafts
brief                   print the block injected at session start
doctor                  check the corpus, the brief's cap and the platform
compact                 compact the object database
debug on|off|tail|clear trace the interactions

hook-start | hook-prompt | hook-guard    hook entry points
```

## Drafts — yours alone

Not everything is a rule yet. `--draft` writes the entry under
`drafts/<you>/` instead:

```sh
cogitex add decision --draft     # same JSON, same validation
cogitex list drafts
cogitex promote <id>             # makes it binding for the team — moves the gate
cogitex drop <id>                # it is yours, so deleting it is the normal move
```

A draft is injected into **your** sessions and nobody else's, it **never blocks
anyone**, and being committed on the same branch it follows you from one machine
to the next. Promotion is a file move: the entry keeps its bytes, and that is the
moment — the only moment — the team is interrupted.

Three properties are worth knowing precisely:

- **It cannot block anyone, by construction.** The gate is
  `git rev-list -- decisions facts`, and a git pathspec is anchored at the tree
  root, so `drafts/…` is invisible to it — the same mechanism that already makes
  `notes/` harmless. There is no exclusion list to keep in sync.
- **Your drafts never cost the team a line.** They get their own budget in the
  injected block (`draftsMaxBytes`, 900 by default). If the team's own rules no
  longer fit, the drafts block disappears entirely rather than push one out.
- **Private by tooling, not by mechanism.** The branch is pushed, so anyone who
  opens the worktree can read someone else's drafts. Nothing injects them,
  searches them or counts them — but no secret belongs in one, ever.

Drafts need an identity, taken from `git config user.email`, overridable with
`COGITEX_ACTOR` or an `actor` key in `.claude/cache/cogitex/config.json` (local,
gitignored). Without one, **no draft is visible at all** — fail-closed, so that two
machines without a git identity never share a namespace.

## Repository layout

```
cmd/cogitex/             the binary's sources (go.mod stays at the root)
    agent.go             the translation into each agent's dialect
build.sh                 rebuilds the five binaries into dist/
install.sh / install.ps1 drop dist/ into a host project

.claude/skills/          THIS repository's own skills: building, packaging

.claude-plugin/          the Claude Code manifest, and the marketplace for both
.plugin/                 the GitHub Copilot manifest
hooks/                   claude-hooks.json and copilot-hooks.json
commands/                the /sync, /find, /doctor and /draft commands

dist/                    what gets dropped into a project, as is
├── .claude/
│   ├── cogitex/
│   │   ├── bin/cogitex-<os>-<arch>  the binaries, ~3.5 MB each
│   │   ├── cogitex.sh / .cmd        pick the binary for the platform
│   │   └── README.md                getting started, on the host project's side
│   ├── agents/cogitex-curator.md    the agent that keeps the corpus in order
│   └── skills/
│       ├── cogitex-recall/          read the context before choosing an approach
│       └── cogitex-record/          record a decision, a fact, a note
└── .github/
    ├── hooks/cogitex.json           the Copilot hooks, committable as they are
    └── instructions/                what Copilot needs to know about cogitex
```

Both manifests point at the **same** skills and the **same** agent, inside
`dist/`: nothing is duplicated, and the binaries live in exactly one place in the
repository.

## Developing

The sources live in `cmd/cogitex/`; `go.mod` stays at the root, where go expects
it. It is an ordinary Go module.

```sh
go build ./...     # compile
go test ./...      # covers the corpus, the state, the rendering and both dialects
./build.sh         # rebuilds the five binaries into dist/
```

`build.sh` is to be run **only when cutting a release**: every run adds one blob
per platform to git history, and git handles a frequently changing binary badly.
The binaries in `dist/` are the ones from the last release; they are not
regenerated on every source change.

Everything an agent reads — the skills, the commands, the curator agent, the
Copilot instructions, and every message a hook hands back to a model — is written
in English. The Go sources are commented in French, as is the installers' output.

## What is tied to an agent, and what is not

The core — corpus, branch, worktree, rendering, search — depends on nothing but
`git`. What assumes an agent fits in two configuration files and one Go file:

| Path | Role |
|---|---|
| `cmd/cogitex/agent.go` | reads both payload dialects, writes both response dialects |
| `hooks/claude-hooks.json` | the three Claude Code events, plugin side |
| `hooks/copilot-hooks.json` | the same ones, under Copilot's names |
| `.claude/settings.json` | a project's Claude Code hooks, merged by `init`, committed |
| `.github/hooks/cogitex.json` | a project's Copilot hooks, committed |
| `.claude/cache/cogitex/` | a session's pinning state |

The differences between the two agents come down to three points, all carried by
`cmd/cogitex/agent.go`:

- Claude sends `session_id` / `tool_name`, Copilot sends `sessionId` / `toolName`;
- Claude expects the guard's decision under `hookSpecificOutput`, Copilot expects
  it at the root of the object;
- at **prompt** time, Claude accepts additional context, Copilot does not. So the
  session stays stale under Copilot, and it is the refusal on the first edit that
  carries the message — the one place where it is certain to arrive.

Porting cogitex to a third agent means adding a dialect to
`cmd/cogitex/agent.go` and one hooks file. Nothing else moves.
