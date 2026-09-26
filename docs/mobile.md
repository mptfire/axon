# Phone & desktop apps

axon speaks the **exact same protocol as upstream ntfy**, so the official apps work
unmodified — point them at your axon server and log in.

## Android / iOS apps

1. Install the open-source ntfy app ([Google Play](https://play.google.com/store/apps/details?id=io.heckel.ntfy),
   [F-Droid](https://f-droid.org/en/packages/io.heckel.ntfy/)) or the iOS app.
2. Add server: `https://axon.rtard.de` (or your own axon host).
3. Log in with your axon account (needed for reserved/protected topics; public topics
   work without login).
4. Subscribe to topics — AI enrichment, translations, digests and scheduled briefings
   arrive as regular notifications.

Everything in the notification path is standard ntfy: priorities, tags, click URLs,
action buttons, attachments, delayed delivery. AI features that change *content*
(enrichment summaries, translations, digests) are applied server-side before delivery,
so they appear on every device — no app update needed.

## Web app

The web app at your axon host includes everything the ntfy web app has, plus the axon
AI features: subscription wizard, per-topic AI tuning, digests and the chat dialog.

## Desktop (browser as app)

Use "Install as app" in Chrome/Edge (or Add to Home Screen on mobile) against your
axon web app for a standalone window with notification support.

## Web Push

For notifications without a running app:

```bash
axon webpush keys    # once, on the server; add keys to server.yml
```

Then enable "Background notifications" in the web app's Settings → Notifications.

## What's intentionally *not* in the apps

The axon AI dialogs (wizard, tuning, digests, chat) are **web-app features** — the
mobile apps are upstream builds and don't include them. Agents should use the
[MCP endpoint](agents.md) instead of a mobile app.
