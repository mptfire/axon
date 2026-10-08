# Security Policy — axon

axon is a fork of [ntfy](https://github.com/binwiederhier/ntfy). Security
issues in ntfy-core-inherited code that also affects upstream ntfy should be
reported upstream per [ntfy's security policy](https://github.com/binwiederhier/ntfy/blob/main/SECURITY.md)
— but when in doubt, report here first.

## Reporting a vulnerability

**Primary channel: GitHub private vulnerability reporting** on this
repository (Security tab → "Report a vulnerability"). This reaches the
maintainer (@mptfire) privately and allows coordinated disclosure.

Please include: affected version/commit, component (device/agent channel,
MCP, AI layer, web, core), impact, and minimal reproduction steps. Do not
open public issues with exploitable details, and strip credentials and
private message content from any logs you attach.

## Supported versions

Only the latest tagged release and current `main` receive security fixes.

## Scope notes

- The device/agent pairing channel, device config sync, MCP server, and AI
  layer are axon-specific code — report here, not upstream.
- Treat files, paths, metadata, automation requests, and message content as
  potentially malicious input.
