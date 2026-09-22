# Shared context

Orphan branch. It carries no code and is never merged into the main branch.

- `decisions/<domain>/<date>-<slug>.yaml` — the rules. Immutable: supersede, never edit.
- `facts/<date>-<slug>.yaml` — volatile facts, expiring on their own (ttl_days).
- `notes/<date>-<slug>.md` — ideas, backlog, and above all dead ends with their reason.
- `journal/<YYYY-MM>/<actor>.ndjson` — who did what. One file per actor.
- `drafts/<actor>/…` — personal. Injected into their author's sessions only,
  never into anyone else's, and they block nobody until `cogitex promote`.
  Readable by anyone who opens this branch: private by tooling, not by mechanism.

Everything here is written in English. Do not hand-edit: the identifier is the path,
and decisions and facts make other people's sessions stale.
