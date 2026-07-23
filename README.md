# opencode-cloak

A [CLIProxyAPI](https://github.com/) plugin that makes third-party `opencode` CLI requests look like Anthropic's official Claude Code, so a Claude Max OAuth subscription proxied through CLIProxyAPI is not flagged as third-party traffic.

opencode-cloak is a Go C-ABI plugin with the `request_interceptor` capability. It hooks `request.intercept_after`, which runs after auth selection and before the executor's native cloaking step.

## How it works

When the plugin detects an opencode request, it performs three transformations:

1. **Surgically sanitizes the opencode system prompt.** It strips the "You are OpenCode" identity paragraph and any paragraphs containing opencode URLs, rewrites two classifier-trigger phrases, and preserves everything else. The rest of the prompt survives untouched.

2. **Relocates the sanitized prompt.** The cleaned prompt moves into a user/assistant message pair at the front of `messages`.

3. **Replaces `system`.** The `system` field becomes:

   ```
   [ <x-anthropic-billing-header block>, "You are Claude Code, Anthropic's official CLI for Claude." ]
   ```

Putting the billing header at `system[0]` makes CLIProxyAPI's native cloaking step aside: it detects the header prefix and skips its own (otherwise destructive) system-prompt replacement. Native cloaking still applies afterward: fake user-id, sensitive-word obfuscation, device-profile User-Agent, and OAuth request signing.

## Activation gates

The plugin transforms a request only when **all** of the following hold. Otherwise the request passes through unchanged.

1. Both the request source format and the target format are `claude`.
2. `system[0]` is not already a billing header (the transform is idempotent).
3. The request is not a real Claude Code request.
4. The model does not start with `claude-3-5-haiku`.
5. There is positive opencode evidence: the client User-Agent matches the configured opencode UA pattern, or a system paragraph contains the exact text "You are OpenCode".

Gate 5 matters. A stray opencode URL or merely "not Claude Code" is not enough to trigger the transform. Cherry Studio, Cline, generic SDKs, and other third-party clients are left untouched.

## Configuration

```yaml
plugins:
  enabled: true
  dir: "/absolute/path/to/opencode-cloak/bin"   # directory containing the built library
  configs:
    opencode-cloak:
      enabled: true
      priority: 1
      claude_code_version: "2.1.63"
      entrypoint: "cli"
      opencode_ua_regex: "(?i)^opencode/"
```

| Field | Type | Default | Meaning |
|---|---|---|---|
| `claude_code_version` | string | `"2.1.63"` | Claude Code version stamped into `x-anthropic-billing-header` (`cc_version`). MUST match the version CLIProxyAPI emits in its outgoing `User-Agent` (Claude device profile); if they drift, the fingerprint becomes internally inconsistent. The default matches CLIProxyAPI's default device profile. |
| `entrypoint` | string | `"cli"` | The `cc_entrypoint` value. `cli` matches the outgoing `claude-cli/… (external, cli)` UA and `X-App: cli`. Use `sdk-cli` only to reproduce claude-relay-service / opencode-anthropic-auth reference vectors. |
| `opencode_ua_regex` | string | `"(?i)^opencode/"` | Regex identifying an opencode client by User-Agent. An invalid regex falls back to the default. |

## Caveats

**Version coupling.** `claude_code_version` must stay in lockstep with the Claude Code version CLIProxyAPI advertises in its device-profile User-Agent. If CLIProxyAPI bumps its device profile and you don't bump this setting (or vice versa), the request fingerprint contradicts itself.

**`cch` re-signing.** On OAuth message requests, CLIProxyAPI re-signs the billing header's `cch` field itself (a seeded hash over the final body), so the plugin's `cch` is authoritative only for `count_tokens`. The durable value of this plugin is the surgical system-prompt preservation (native cloaking would otherwise replace the whole opencode prompt with a 3-line stub) plus the `cc_version`/suffix/`cc_entrypoint` fingerprint.

## Build & Install

Build the shared library:

```bash
make build
```

Or build directly:

```bash
# macOS
go build -buildmode=c-shared -o bin/opencode-cloak.dylib .

# Linux: use .so, Windows: use .dll
go build -buildmode=c-shared -o bin/opencode-cloak.so .
```

Install:

1. Place the built library in the directory named by `plugins.dir` in your CLIProxyAPI config. The file basename becomes the plugin ID, so keep it `opencode-cloak`.
2. Set `plugins.enabled: true`.
3. Enable the plugin under `plugins.configs.opencode-cloak` as shown in the Configuration section.

## ⚠️ Terms of Service / Risk

Using this plugin may violate Anthropic's Terms of Service. It is provided for technical research and learning purposes only. You assume all risk, including account bans, service interruption, and any other consequences of use.
