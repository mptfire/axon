# AI agents (MCP)

axon ships a built-in [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server,
so AI agents — Claude Desktop, Claude Code, IDE agents, scripts — can use your ntfy server as
their notification transport: send push notifications to your phone, wait for replies, read
history, and generate subscription plans.

The MCP layer is a **thin client of the public ntfy API**: it authenticates with a regular
access token, inherits all rate limits, access control and AI budgets, and never touches
server internals.

## Quick start

```bash
ntfy mcp                                    # talk to https://ntfy.sh, anonymous
ntfy mcp -s https://ntfy.example.com        # your self-hosted server
ntfy mcp -s https://ntfy.example.com -k tk_abc123   # authenticated
```

The server speaks JSON-RPC over stdio (the MCP stdio transport). Logs go to stderr.

### Claude Desktop

Add to `claude_desktop_config.json`:

``` json
{
  "mcpServers": {
    "ntfy": {
      "command": "ntfy",
      "args": ["mcp", "--server", "https://ntfy.example.com", "--token", "tk_..."]
    }
  }
}
```

### Claude Code / other CLI agents

```bash
claude mcp add ntfy -- ntfy mcp --server https://ntfy.example.com --token tk_...
```

## Tools

| Tool                 | Auth needed | What it does |
|----------------------|-------------|--------------|
| `publish`            | -           | Publish a notification (topic, message, title, priority 1-5, tags, scheduled delay) |
| `read_messages`      | -           | Replay recent cached messages from a topic (`since=all`, duration, or unix time) |
| `subscribe_wait`     | -           | Block until the next live message arrives on a topic (timeout-capped) |
| `list_subscriptions` | token       | List the account's synced subscriptions |
| `digest_topic`       | token       | AI summary of one topic's recent messages |
| `briefing`           | token       | AI summary across all of the account's topics ("what did I miss?") |
| `plan_subscription`  | -           | Turn a wish like "notify me when backups fail" into a subscription plan (AI layer required on the server) |

### Human-in-the-loop pattern

The classic agent loop with axon: the agent publishes a message with
[action buttons](../publish.md#action-buttons) (`publish` + ntfy's `X-Actions` header), the
human taps **approve**/**deny** on their phone, and the button publishes back to a reply
topic the agent is waiting on with `subscribe_wait`. That's the whole round trip — no
webhooks, no public agent endpoint, no polling.

## Safety notes

- Create a dedicated access token for your agent (`ntfy token add`) and treat it as the
  agent's identity; revoke it to cut the agent off. Server-side AI token budgets
  (`ai-visitor-daily-token-budget`) apply per token.
- **Scoped tokens** (axon): give an agent only what it needs with
  `ntfy token add --scope=ai phil`. A token created with any `--scope` list can *only*
  perform the listed capabilities — `--scope=ai` allows the AI endpoints (plan, tune,
  digest, chat) but denies nothing else by itself, while a token *without* the `ai`
  scope can publish/subscribe but never invoke AI features. Tokens created without
  `--scope` remain fully unrestricted (backwards compatible).
- `plan_subscription` only *proposes* plans — subscribing, and everything else the agent
  does, should still be reviewed by a human (see the [fork plan](ai-plan/index.md),
  Principle 2).
- Topic names are validated client-side (1-64 chars, letters/digits/`-`/`_`); the agent
  can only touch topics its credentials allow.
