---
name: cogitex-build
description: Build, test and release this repository's cogitex binary. Use whenever touching the Go code under cmd/cogitex/, running the tests, or about to regenerate the five binaries in dist/ — the release command, which is not run lightly.
---

# Building cogitex

The repository is an ordinary Go module: `go.mod` at the root, sources under
`cmd/cogitex/`. No external dependency, no code generation, nothing to install
beyond the toolchain.

```sh
go build ./...     # compile
go vet ./...       # run it before calling anything finished
go test ./...      # ~20 s: corpus, state, rendering, both hook dialects
```

The tests build the binary on the fly and drive it against real temporary git
repositories. They are slow and exactly representative: if a guard test passes,
the matching hook works.

## The rule that is expensive to ignore

**`./build.sh` runs only when cutting a release.** It writes five binaries of
about 3.5 MB each into `dist/.claude/cogitex/bin/`, and **every run adds five
blobs to git history** — which git cannot deduplicate from one version to the
next. A repository rebuilt on every commit becomes unclonable within weeks.

Changing Go code therefore **does not oblige** you to regenerate `dist/`. The
committed binaries are the ones from the last release; they are allowed to lag
behind the sources, and the README says so.

When a release is actually decided:

```sh
go vet ./... && go test ./... && ./build.sh
```

**Never build a release on a red test suite.** The binaries in `dist/` are what
every host project runs; a release cut over a failing test ships the failure to
all of them, and the next release is the only way to take it back. Some tests
depend on the local git version (the `relativeWorktrees` ones do), so a suite
that passed last month can turn red after a git upgrade — rerun it, do not trust
an old green.

It crosses `CGO_ENABLED=0` with five GOOS/GOARCH pairs (darwin arm64 and amd64,
linux amd64 and arm64, windows amd64). `CGO_ENABLED=0` is not decoration: it is
what guarantees a binary with no glibc, no musl, nothing to find on a teammate's
machine — the project's central promise.

## Two Windows traps that are invisible on Linux

1. **`install.ps1` must keep its UTF-8 BOM.** Windows PowerShell 5.1 reads a
   BOM-less file as CP1252, where the em dash `—` (`E2 80 94`) decodes to `â€”` —
   and that `0x94` is a *right double quotation mark*, which PowerShell treats as
   a string delimiter. The whole script then fails to parse, on an error pointing
   at a brace thirty lines earlier. `.gitattributes` pins `*.ps1 text eol=crlf`,
   and the BOM is part of the file's bytes: never re-save that file as plain
   UTF-8. Check it with:

   ```sh
   head -c 3 install.ps1 | xxd     # expect efbbbf
   ```

2. **PowerShell prefixes a BOM to a native command's stdin.** `Get-Content x.json
   | cogitex.cmd add decision` hands the binary three invisible bytes before the
   `{`. `readAllStdin()` and `readHookInput()` strip them; the regression test is
   `TestStdinToleratesAUTF8BOM`. Any new stdin reader has to do the same.

And one that used to bite: under Git Bash, `uname -s` says
`MINGW64_NT-10.0-26200`, not `windows`. `cogitex.sh` maps `mingw*`, `msys*` and
`cygwin*` to `windows` plus a `.exe` suffix — without that mapping, the shim
looks for a binary that exists for nobody, and it is exactly the shim a plugin
hook invokes on Windows.

## After a build: two more traps

1. **The executable bit.** The Unix binaries and `cogitex.sh` must stay `100755`
   in the git index, otherwise a Linux clone gets a launcher that refuses to
   start. On a Windows machine `core.filemode=false`: the mode does not follow the
   file, it has to be set by hand.

   ```sh
   git update-index --chmod=+x dist/.claude/cogitex/cogitex.sh \
     dist/.claude/cogitex/bin/cogitex-darwin-amd64 \
     dist/.claude/cogitex/bin/cogitex-darwin-arm64 \
     dist/.claude/cogitex/bin/cogitex-linux-amd64 \
     dist/.claude/cogitex/bin/cogitex-linux-arm64
   git ls-files -s dist/.claude/cogitex/   # expect 100755 everywhere but .exe, .cmd, .md
   ```

2. **Line endings.** `.gitattributes` declares `dist/.claude/cogitex/bin/**
   binary` and `*.sh text eol=lf`. Without the first line, a Windows clone
   corrupts the binaries; without the second, `cogitex.sh`'s shebang carries a
   `\r` and the script becomes unexecutable under Unix. Do not touch either
   without a reason.

## Verifying for real

A unit test does not tell you whether the installed package works. The check that
counts is a handful of commands in a throwaway repository:

```sh
D=$(mktemp -d); git -C "$D" init -q .; git -C "$D" commit -q --allow-empty -m init
./install.sh "$D"
cd "$D" && .claude/cogitex/cogitex.sh init
echo '{"title":"T","type":"convention","domain":"api","scope":"global","decision":"A rule in one sentence.","rationale":"Why."}' \
  | .claude/cogitex/cogitex.sh add decision --no-push
echo '{"session_id":"s1","source":"startup"}' | .claude/cogitex/cogitex.sh hook-start "$D"
echo '{"sessionId":"c1","source":"startup"}'  | .claude/cogitex/cogitex.sh hook-start "$D"
```

Each of the last two lines must print **exactly one JSON line**: one wrapped in
`hookSpecificOutput`, the other with `additionalContext` at the root. That is the
signature of the two dialects — see the `cogitex-plugin` skill.

One caution when building a guard scenario: `cogitex add` **re-pins the most
recent session**, so that whoever records a rule is not blocked by their own rule.
A session created just before an `add` will therefore not be stale, and the guard
is right to stay silent.
