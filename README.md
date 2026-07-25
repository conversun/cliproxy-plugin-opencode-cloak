# opencode-cloak

A [CLIProxyAPI](https://github.com/) plugin that makes third-party `opencode` CLI requests look like Anthropic's official Claude Code, so a Claude Max OAuth subscription proxied through CLIProxyAPI is not flagged as third-party traffic — **while preserving opencode's own system prompt**.

opencode-cloak is a Go C-ABI plugin with the `request_interceptor` capability. It hooks `request.intercept_after`, which runs after auth selection and before the executor's native cloaking step.

It preserves opencode's own system prompt while aligning its layout with the current `opencode-anthropic-auth` strategy. CLIProxyAPI still owns the outgoing device headers, fake user-id, sensitive-word obfuscation, and final-body `cch` re-signing.

## How it works

When the plugin detects an opencode request, it performs three transformations:

1. **Surgically sanitizes the opencode system prompt.** It strips the "You are OpenCode" identity paragraph and any paragraphs containing opencode URLs, rewrites two classifier-trigger phrases, and preserves everything else.

2. **Keeps the sanitized prompt in `system`.** Original system blocks retain their order and metadata (including `cache_control`). The conversation in `messages` is not modified.

3. **Prepends the Claude fingerprint blocks.** The final system layout is:

```
[ <x-anthropic-billing-header>, "You are Claude Code, Anthropic's official CLI for Claude.", <sanitized original blocks...> ]
```

Putting the billing header at `system[0]` makes CLIProxyAPI's native system-prompt replacement step aside. Native processing still injects the fake user-id, applies the outgoing Claude device profile, and re-signs `cch` over the final upstream body.

### Division of labor

| Concern | Owner |
|---|---|
| Sanitize and preserve opencode's system blocks | **this plugin** |
| Billing-header version, suffix, and entrypoint | **this plugin** (must match the host tuple) |
| `cch` signing over the final body | native (re-signs for OAuth requests) |
| Fake user-id, device User-Agent, sensitive-word obfuscation | native |

This deployment uses the CLI tuple observed from CLIProxyAPIPlus: `claude-cli/2.1.63 (external, cli)`, `X-App: cli`, `cc_version=2.1.63.*`, and `cc_entrypoint=cli`.

## Activation gates

The plugin transforms a request only when **all** of the following hold. Otherwise the request passes through unchanged (and native cloaks it normally).

1. Both the request source format and the target format are `claude`.
2. `system[0]` is not already a billing header (the transform is idempotent).
3. The request is not a real Claude Code request.
4. The model does not start with `claude-3-5-haiku` (native skips cloaking there too, so the plugin must not strip the prompt).
5. There is positive opencode evidence: the client User-Agent matches the configured opencode UA pattern, or a system paragraph contains the exact text "You are OpenCode".
6. The request has a user message from which a valid billing header can be derived.

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
| `claude_code_version` | string | `"2.1.63"` | Version used in the billing header. It must match CLIProxyAPI's final outgoing Claude CLI User-Agent. |
| `entrypoint` | string | `"cli"` | Billing-header entrypoint. Use `cli` with CLIProxyAPI's current `X-App: cli` tuple. |
| `opencode_ua_regex` | string | `"(?i)^opencode/"` | Regex identifying an opencode client by User-Agent. An invalid regex falls back to the default. |

## Caveats

**Version coupling.** `claude_code_version` must match the final outgoing Claude CLI User-Agent emitted by CLIProxyAPI. A mismatch creates an internally contradictory fingerprint. Check the upstream request log after host upgrades.

**No entitlement guarantee.** This layout improves consistency and follows the current upstream auth-plugin direction, but Anthropic can still classify OpenCode or agent frameworks as third-party traffic. Disable this plugin to fall back to native cloaking if plan-limit compatibility is more important than prompt fidelity.

## Install

### Option A - Prebuilt binary (recommended)

Every release ships prebuilt shared libraries built by CI for common platforms:
`opencode-cloak-linux-amd64.so`, `opencode-cloak-linux-arm64.so`,
`opencode-cloak-darwin-arm64.dylib`, `opencode-cloak-darwin-amd64.dylib`.

Download the one matching your server from the
[Releases page](https://github.com/conversun/cliproxy-plugin-opencode-cloak/releases),
rename it to `opencode-cloak.<ext>` (keep the basename `opencode-cloak` - it becomes the
plugin ID), and place it in the directory named by `plugins.dir`:

```bash
mkdir -p /opt/cliproxy/plugins
curl -L -o /opt/cliproxy/plugins/opencode-cloak.so \
  https://github.com/conversun/cliproxy-plugin-opencode-cloak/releases/latest/download/opencode-cloak-linux-amd64.so
```

### Option B - Build from source

No local CLIProxyAPI checkout is required: the plugin depends on the **public** SDK module
(`github.com/router-for-me/CLIProxyAPI/v7`), so a plain build works anywhere (Go 1.26+, CGO enabled):

```bash
git clone https://github.com/conversun/cliproxy-plugin-opencode-cloak.git
cd cliproxy-plugin-opencode-cloak
make build   # or: CGO_ENABLED=1 go build -buildmode=c-shared -o bin/opencode-cloak.so .
```

`make build` selects the extension per OS (`.so` Linux, `.dylib` macOS, `.dll` Windows). Build the
library on the **same OS/arch** as the host that loads it.

### Enable it

1. Place the library in the directory named by `plugins.dir` (basename stays `opencode-cloak`).
2. Set `plugins.enabled: true`.
3. Enable it under `plugins.configs.opencode-cloak` (see Configuration).

> ABI: the plugin targets CLIProxyAPI plugin ABI v1 (built against SDK v7.2.96) and loads into any host that speaks ABI v1.

## ⚠️ Terms of Service / Risk

Using this plugin may violate Anthropic's Terms of Service. It is provided for technical research and learning purposes only. You assume all risk, including account bans, service interruption, and any other consequences of use.
