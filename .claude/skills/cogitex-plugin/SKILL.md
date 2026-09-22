---
name: cogitex-plugin
description: How cogitex is packaged as a plugin for Claude Code and for GitHub Copilot — the two manifests, the two hook files, what must stay in sync and what must never be duplicated. Use when touching the hooks, the manifests, the shipped skills, or when porting cogitex to a third agent.
---

# The plugin, repository side

The repository root **is** the plugin root, and it is also its own marketplace.
Nothing is duplicated: both agents point at the same skills, the same curator
agent and the same binaries, inside `dist/`.

| File | Read by |
|---|---|
| `.claude-plugin/plugin.json` | Claude Code |
| `.claude-plugin/marketplace.json` | Claude Code **and** Copilot CLI |
| `.plugin/plugin.json` | Copilot CLI and VS Code (legacy format, free component paths) |
| `hooks/claude-hooks.json` | declared by the Claude manifest |
| `hooks/copilot-hooks.json` | declared by the Copilot manifest |
| `commands/*.md` | the `/sync`, `/find` and `/doctor` commands |

Two manifests, because Copilot looks for `.plugin/plugin.json` **before**
`.claude-plugin/plugin.json`: each client finds its own, and neither reads the
other's events. It is also why the hook files are not named `hooks/hooks.json`,
the default name for both clients — it would be loaded by both.

## The three differences between the agents

All of it lives in `cmd/cogitex/agent.go`. The rest of the binary does not know
which agent is calling.

1. **The payload.** Claude sends `session_id`, `tool_name`, `tool_input`; Copilot
   sends `sessionId`, `toolName`, `toolArgs`. `readHookInput` accepts both and
   infers the agent from the camelCase, unless `--copilot` / `--claude` settles
   it.
2. **The response.** Claude expects the guard's decision under
   `hookSpecificOutput`; Copilot expects it at the root of the object. Getting it
   wrong raises **no error**: the decision is silently ignored and the guard
   becomes decorative. That is exactly what `TestCopilotGuardSpeaksItsOwnDialect`
   covers.
3. **The prompt.** Claude accepts additional context at `UserPromptSubmit`;
   Copilot has no field for it (`modifiedPrompt` is honored only by the SDK's
   programmatic hooks). Under Copilot the session therefore **stays stale** after
   a prompt: it is the refusal on the first edit that carries the message. Never
   re-pin a session that was shown nothing — that would open the guard on a delta
   nobody read.

The event names differ too (`SessionStart` / `sessionStart`, `PreToolUse` /
`preToolUse`), but that lives only in the two JSON files.

## Double wiring, and who wins

A project may have its own hooks — written by `init` into
`.claude/settings.local.json`, or committed in `.github/hooks/cogitex.json` —
while the plugin is installed globally. With no rule, every turn would pay for
the same hook twice: the brief injected twice, and **two refusals counted for a
single write**, which trips the circuit breaker too early.

The rule is in `pluginStandsDown()`: if the binary comes from a plugin and the
project already wires its hooks, the hook exits without writing anything.
Symmetrically, `cmdInit()` wires nothing when it runs from a plugin.

## Touching the guard's matcher

The Copilot matcher is compiled as `^(?:PATTERN)$` **in JavaScript**: no `(?i)`,
no Go syntax. It has to cover both clients' write-tool names (`create`, `edit`,
`write`, `str_replace`, `apply_patch`, `insert_edit_into_file`,
`replace_string_in_file`…) without ever catching `bash`: the guard protects
edits, not the shell — that is the parity with `Edit|Write|MultiEdit` on the
Claude side.

## What moves together

A change to the entry points almost always touches **five** places:

1. `cmd/cogitex/agent.go` or `hooks.go` — the policy and its translation;
2. `hooks/claude-hooks.json` and `hooks/copilot-hooks.json` — the plugin;
3. `dist/.github/hooks/cogitex.json` — a project's committed Copilot wiring;
4. `writeSettings()` in `cmd/cogitex/main.go` — a project's Claude wiring;
5. the README table, which lists those paths.

Missing one breaks nothing loudly: it simply leaves one agent without a guard.
Re-reading the five afterwards is faster than discovering it in production.

## Validating the manifests

```sh
/plugin validate .          # in Claude Code
```

And, by hand, check that every declared path really exists: the `skills`,
`agents` and `hooks` fields point into `dist/` and into `hooks/`, and a dead path
is silently ignored by both clients.
