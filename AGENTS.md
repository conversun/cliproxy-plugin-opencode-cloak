# AGENTS.md

## What this repo is

A **C-ABI shared library**, not a program. Single flat Go package `main` at the repo root, built with
`-buildmode=c-shared` and loaded via `dlopen` by a CLIProxyAPI host. `func main() {}` is intentionally
empty — never "fix" it. Read `README.md` for the product-level contract (activation gates, config
fields, division of labor with the host).

## Commands

```bash
make build   # go build -buildmode=c-shared -o bin/opencode-cloak.$EXT .  (EXT auto: dylib/so/dll)
make test    # go test ./... -count=1
make vet     # go vet ./...
make fmt     # gofmt -w .
```

CI (`.github/workflows/ci.yml`) runs, in order: **gofmt check → go vet → go test → c-shared build**,
on ubuntu-latest + macos-latest. The gofmt gate is `test -z "$(gofmt -l .)"` — any unformatted file
fails the build, so run `make fmt` before finishing.

Single test: `go test -run TestName -v`. There is no lint tool beyond `gofmt` + `go vet`.

## Build constraints

- **CGO is mandatory** (`CGO_ENABLED=1`); `make build` relies on the default being on.
- **Cross-compilation does not work.** `GOOS=linux go build` on macOS fails in `runtime/cgo`. Release
  artifacts come from a 4-runner matrix in `.github/workflows/release.yml` (linux/darwin × amd64/arm64),
  triggered by a `v*` tag. Never try to produce another platform's `.so`/`.dylib` locally.
- `bin/` is gitignored; `bin/opencode-cloak.h` is cgo-generated output, not source.

## File map

| File | Role |
|---|---|
| `main.go` | C ABI boundary (cgo preamble, exported symbols), `handleMethod` dispatch, YAML config parsing, `plugin.register` metadata |
| `transform.go` | Activation gates + `system[]` rewrite; holds `currentConfig` (atomic pointer) |
| `cloaking.go` | Paragraph-level sanitizer for opencode system text |
| `cch.go` | Billing-header construction: `cch` hash + version suffix |
| `useragent.go` | Canonical Claude Code User-Agent detection (gate only) |

## Host contract invariants (breaking these fails silently in production)

- **Registration metadata must be complete.** The host's `validPlugin()` rejects the plugin unless
  `Metadata.Name`, `Version`, `Author`, **and** `GitHubRepository` are all non-empty *and* at least one
  capability is true. A failing plugin still `dlopen`s successfully and then never registers — no loud
  error. Locked by `registration_test.go`; a prior commit shipped this bug.
- **Never return an error or panic across the ABI.** Every failure path returns a no-op envelope
  (`okEnvelope(pluginapi.RequestInterceptResponse{})`). Both `interceptAfter` and
  `transformInterceptAfter` have `defer recover()` handlers that degrade to no-op. A panic here would
  take down the host process, not just the request.
- **Exported symbol names are wired by hand.** `cliproxyPluginCall` / `cliproxyPluginFree` /
  `cliproxyPluginShutdown` are declared `extern` in main.go's cgo preamble and re-referenced in
  `cliproxy_plugin_init`. Renaming a Go function requires editing the C preamble in the same file.
- `SchemaVersion` / `ABIVersion` come from `pluginabi`; do not hardcode.
- `interceptBefore` is a deliberate no-op. All cloaking must stay in `intercept_after`, which runs
  *after* auth selection. Do not "implement" the before-hook.

## Transform invariants

- **Idempotency**: if `system[0]` already starts with `x-anthropic-billing-header`, bail out. The host
  may invoke the hook more than once.
- **Fail closed, not open**: any gate miss returns `(nil, false)` so native cloaking handles the request
  unchanged. Adding a transform path that partially mutates on failure is a regression.
- **`cch` hashing follows JavaScript string semantics, not Go's.** It samples **UTF-16 code units** at
  positions `{4, 7, 20}`, pads out-of-range positions with the ASCII character `'0'`, and lets adjacent
  sampled surrogate halves **recombine into one rune** before the UTF-8 hash. Indexing `[]rune` or
  `[]byte` instead silently breaks parity. Golden values are pinned in `cch_test.go` (`4ffc3`, `6ff`).
- `extractFirstUserMessageText` returns the first `type:"text"` block of the first `user` message —
  skipping leading non-text blocks but **not** skipping an empty text block. This asymmetry is
  intentional reference-implementation parity and is test-locked.
- `versionPattern` / `entrypointPattern` in `cch.go` are defense-in-depth against billing-header
  injection (`version: "2.1.87;cc_entrypoint=attacker"`). Do not relax them.

## Version coupling

The fallback Claude CLI User-Agent lives in `defaultClaudeCodeUserAgent` in `transform.go` and the
documented default in `README.md`. Production configuration should define
`plugins.configs.opencode-cloak.claude_code_user_agent` with a YAML anchor and reuse that anchor for
`claude-header-defaults.user-agent`, so the plugin and host consume one value. A mismatch produces a
self-contradictory fingerprint.
Separately, `Metadata.Version` in `main.go` (`pluginRegistration`) is hand-maintained and is *not*
derived from the git tag that triggers a release.

## Testing conventions

- Names: `TestSubject_behavior_whenCondition`; bodies use `// Given` / `// When` / `// Then` comments.
- Use the `newTransformRequest` helper (`transform_test.go`) and `unwrapInterceptResponse`
  (`intercept_after_test.go`) rather than hand-building envelopes.
- `TestHarnessOpencodeRealistic` is a **QA evidence generator**, not a unit test: run
  `go test -run TestHarnessOpencodeRealistic -v` and its stdout is the artifact. `docs/qa-evidence/
  harness-output.txt` is a checked-in snapshot that goes stale whenever the transform contract changes —
  regenerate it in the same commit as any layout change.

## Repo hygiene

- `.gitignore` only contains `bin/`. `.codegraph` is excluded locally via `.git/info/exclude`, but
  `.omo/` is **not ignored anywhere** — `git add -A` will stage agent scratch state. Stage files
  explicitly.
