# Agent setup runbook

Everything an AI agent needs to operate an axon instance and its Android app.
Audience: the agent itself. Humans: see docs/agents.md for the design.

## 1. Connect

Add the axon MCP server to your client config:

```json
{
  "mcpServers": {
    "axon": {
      "url": "https://<server>/mcp",
      "headers": { "Authorization": "Bearer tk_..." }
    }
  }
}
```

The token runs as its owner — their ACLs, their quotas (10 AI requests/day,
per-visitor rate limits, batches capped at 10 JSON-RPC messages).

## 2. Tools (11)

| Tool | What it does |
|---|---|
| `publish` | Send a notification (title, priority 1–5, tags, delay) |
| `read_messages` | Replay a topic's cached messages (since windows) |
| `subscribe_wait` | Long-poll for the next message on a topic |
| `ask_history` | Q&A over a topic's history, with citations (AI) |
| `digest_topic` | Summarize one topic (AI) |
| `briefing` | Cross-topic summary of everything the account can read (AI) |
| `list_subscriptions` | The account's synced topic list |
| `plan_subscription` | Natural language → subscription plan (AI, advisory) |
| `request_pairing` | Mint a one-time pairing code (manual/consent flow) |
| `set_device_config` | Configure the paired phone app (the main control surface) |
| `device_status` | List paired devices with last-sync times |

## 3. First-time app setup

Private builds pair themselves: the app holds a baked pairing key and, on its
first launch, claims a device-scoped token with no interaction. As the agent
you only verify and configure:

1. `device_status` — a `dv_...` device with a recent `last sync` means the app
   is paired. If empty: the app was not launched yet (ask the user to open it
   once — Android freezes fresh installs until first launch), or use
   `request_pairing` and have the `axon://pair/<code>?auto=1` link opened on
   the phone.
2. `set_device_config` — push the full subscription surface in one call.

## 4. set_device_config schema

```json
{
  "device_id": "dv_...",
  "config": {
    "manage": "full",
    "subscriptions": [
      {"topic": "alerts", "min_priority": 4, "insistent": true},
      {"topic": "reports", "muted": true, "auto_delete_seconds": 604800,
       "display_name": "Weekly Reports",
       "base_url": "https://other-ntfy.example.com"}
    ]
  }
}
```

- The config **replaces** the previous one — always send the full list.
- Per-topic: `muted`, `min_priority`, `auto_delete_seconds`, `insistent`,
  `display_name`. Omitted fields keep their current value.
- `base_url` (optional) targets another ntfy server the phone has credentials
  for; omit it for this server.
- `manage: "full"` authorizes removals: subscriptions on THIS server that are
  absent from the config get removed on sync. Foreign servers are never pruned.
- The app applies the config on its next sync (background worker, ≤15 min;
  immediately when the app is opened). Subscriptions replay cached messages
  (`since=all`), so history arrives as notifications right away.

## 5. Verify the loop

```
device_status          → last sync advancing
publish → upalerts-ish topic the phone subscribes to → phone buzzes
```

## 6. Rules of the road

- Devices are sandboxed: the app's token can never read the account's other
  tokens, mint tokens, or change account settings. Neither can you, through it.
- AI tools share the owner's daily AI budget — batch your questions.
- Revocation: the user deletes the device in the web UI/API; the app re-pairs
  itself on next launch if the build key remains configured server-side.
- Everything you configure is visible in the app UI — nothing is hidden from
  the user.
