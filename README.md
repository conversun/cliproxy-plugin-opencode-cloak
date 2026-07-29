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

The current production tuple is `claude-cli/2.1.220 (external, sdk-cli)`, `X-App: cli`, `cc_version=2.1.220.*`, and `cc_entrypoint=sdk-cli`.

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

The plugin runs with just `enabled: true`; every other field is optional.

```yaml
plugins:
  enabled: true
  dir: "/absolute/path/to/opencode-cloak/bin"   # directory containing the built library
  configs:
    opencode-cloak:
      enabled: true
      claude_code_user_agent: &claude_code_ua "claude-cli/2.1.220 (external, sdk-cli)"

claude-header-defaults:
  user-agent: *claude_code_ua   # same value as the plugin's claude_code_user_agent
```

| Field | Type | Default | Meaning |
|---|---|---|---|
| `claude_code_user_agent` | string | `"claude-cli/2.1.63 (external, cli)"` | Optional. Claude Code identity (version + entrypoint) the plugin writes into the billing header. If you override it, set `claude-header-defaults.user-agent` to the same value. |
| `opencode_ua_regex` | string | `"(?i)^opencode/"` | Optional. Regex that flags a client as opencode by its User-Agent. Invalid regex falls back to the default. |

## Caveats

**Keep the fingerprint consistent.** The plugin reads only `claude_code_user_agent`, using it to build the billing header. The host's `claude-header-defaults.user-agent` sets the outgoing HTTP User-Agent — match the two or the upstream request contradicts itself. If you use a YAML anchor, declare it inside `plugins.configs.opencode-cloak`: CLIProxyAPI serializes the plugin subtree on its own, so an anchor declared under `claude-header-defaults` won't resolve for the plugin.

**No entitlement guarantee.** This layout improves consistency and follows the current upstream auth-plugin direction, but Anthropic can still classify OpenCode or agent frameworks as third-party traffic. Disable this plugin to fall back to native cloaking if plan-limit compatibility is more important than prompt fidelity.

## Install

### Option A - Plugin store (recommended)

This plugin is listed in the [official CLIProxyAPI plugin store](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store).
Install it through the management API, which downloads the platform archive from
this repo's GitHub Releases, verifies its `sha256`, and drops the library into
`plugins.dir` for you:

```bash
curl -X POST \
  "http://127.0.0.1:8080/v0/management/plugin-store/opencode-cloak/install?source=official" \
  -H "Authorization: Bearer $CLIPROXY_MANAGEMENT_KEY"
```

The store resolves the latest release tag as the version, so re-running the
install (or the management panel's update action) picks up new releases with no
manual download. Then enable it (see [Enable it](#enable-it)).

### Option B - Prebuilt archive

Every release ships a per-platform zip plus a `checksums.txt`, named the way the
store installer expects:
`opencode-cloak_<version>_linux_amd64.zip`, `..._linux_arm64.zip`,
`..._darwin_arm64.zip`, `..._darwin_amd64.zip`, `..._windows_amd64.zip`.

Download the one matching your server from the
[Releases page](https://github.com/conversun/cliproxy-plugin-opencode-cloak/releases),
unzip it (the archive contains a single `opencode-cloak.<ext>` at its root - keep
that basename, it becomes the plugin ID), and place the library in the directory
named by `plugins.dir`:

```bash
mkdir -p /opt/cliproxy/plugins
ver=0.2.1   # match the release tag without the leading v
curl -L -o /tmp/opencode-cloak.zip \
  "https://github.com/conversun/cliproxy-plugin-opencode-cloak/releases/download/v${ver}/opencode-cloak_${ver}_linux_amd64.zip"
unzip -o /tmp/opencode-cloak.zip -d /opt/cliproxy/plugins
```

### Option C - Build from source

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

1. Install via the store (Option A places the library for you), or drop the library into the directory named by `plugins.dir` (basename stays `opencode-cloak`).
2. Set `plugins.enabled: true`.
3. Enable it under `plugins.configs.opencode-cloak` (see Configuration).

> ABI: the plugin targets CLIProxyAPI plugin ABI v1 (built against SDK v7.2.96) and loads into any host that speaks ABI v1.

## ⚠️ Terms of Service / Risk

Using this plugin may violate Anthropic's Terms of Service. It is provided for technical research and learning purposes only. You assume all risk, including account bans, service interruption, and any other consequences of use.
