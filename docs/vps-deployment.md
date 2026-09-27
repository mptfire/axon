# VPS deployment runbook

Step-by-step plan for deploying and operating axon on a public VPS behind an existing
Caddy reverse proxy. Written for a Debian/Ubuntu host with Docker installed; substitute
your values everywhere you see `axon.example.com`, `/opt/axon`, or `<user>`.

> **Privacy note**: this page is in the public repo. It intentionally contains no real
> hostnames, IPs, tokens, or keys — keep it that way. Keep your private values (domain,
> IPs, keys) in your own notes or an untracked file.

---

## 1. Architecture

```
Internet ──► :80/:443 Caddy (TLS, existing) ──► axon:2587 (docker container)
                                                    │  web app + API + AI + MCP (embedded)
                                                    ├── /var/lib/ntfy (cache, auth DB, attachments)
                                                    └── Poolside key file (AI provider)
DNS: axon.example.com  A  <VPS-IP>
```

- Caddy terminates TLS (automatic Let's Encrypt) and proxies to the axon container.
- The axon binary is fully static — it runs in a plain `debian:stable-slim` container
  with zero dependencies (SQLite and the IANA timezone DB are embedded).
- AI runs server-side (Poolside/Ollama/OpenAI-compatible); users never see provider keys.

## 2. Prerequisites

- [ ] A domain A/AAAA record pointing at the VPS IP (e.g. `axon.example.com`)
- [ ] Ports 80 + 443 open in the firewall and reachable
- [ ] Docker + Docker Compose installed (`docker compose version`)
- [ ] The axon container's network must match the network your Caddy container uses
      (`docker network ls`; in this runbook: the Caddy-owned network)
- [ ] A Poolside API key (`sky_...`) if you want AI features — optional; axon works
      fully without AI

## 3. Files layout (`/opt/axon`)

```
/opt/axon/
├── axon                  # static binary (linux-amd64 or linux-arm64)
├── server.yml            # config (see §5)
├── poolside.key          # AI provider key file, chmod 600        (optional)
├── docker-compose.yml    # container definition (see §6)
└── data/                 # cache.db, user.db, attachments (bind mount → /var/lib/ntfy)
```

## 4. Get the binary

From the [releases page](https://github.com/mptfire/axon/releases) (static, no deps):

```bash
curl -LO https://github.com/mptfire/axon/releases/latest/download/axon-linux-amd64.gz
gunzip axon-linux-amd64.gz && chmod +x axon-linux-amd64
```

(arm64: download `axon-linux-arm64.gz` instead — same flags, fully static.)

## 5. `server.yml`

``` yaml
base-url: "https://axon.example.com"
listen-http: ":2587"
behind-proxy: true

cache-file: "/var/lib/ntfy/cache.db"
cache-duration: "12h"
attachment-cache-dir: "/var/lib/ntfy/attachments"
auth-file: "/var/lib/ntfy/user.db"
auth-default-access: "deny-all"      # every topic requires explicit grant
enable-login: true
enable-signup: true                  # flip to false after your accounts exist
enable-reservations: true

# AI (optional — Poolside example; key file is chmod 600, never in the repo)
ai-enabled: true
ai-provider: poolside
ai-base-url: https://inference.poolside.ai/v1
ai-api-key-file: /var/lib/ntfy/poolside.key
ai-model: poolside/laguna-s-2.1
# Semantic chat retrieval (needs an embeddings-capable provider; Ollama: nomic-embed-text)
# ai-embeddings-model: nomic-embed-text

# MCP endpoint for AI agents (POST https://your-host/mcp)
enable-mcp: true
```

Key rules:

- `ai-api-key-file` over `ai-api-key` — the key never sits inside the config that
  support tools might print.
- `auth-default-access: deny-all` — the MCP endpoint and web app rely on explicit
  tokens/grants; this is the single most important hardening setting.
- Budgets are on by default (`ai-visitor-daily-token-budget: 20000`,
  `ai-global-daily-token-budget: 2000000`) — tune per instance.

## 6. Container

Two supported variants:

**a) Hardened image (recommended)** — non-root user, built by CI on every `v*-axon*`
tag and published to GHCR:

``` yaml
services:
  axon:
    image: ghcr.io/mptfire/axon:latest
    restart: unless-stopped
    command: ["serve", "--config", "/etc/ntfy/server.yml"]
    user: "100:101"                        # match the image's ntfy uid/gid (docker run --rm IMAGE id)
    volumes:
      - ./server.yml:/etc/ntfy/server.yml:ro
      - ./data:/var/lib/ntfy
    networks:
      - caddy_net

networks:
  caddy_net:
    external: true
    name: <the network your Caddy container is on>
```

The bind-mounted `./data` must be writable by the container uid:
`sudo chown -R 100:101 ./data` (once, after first pull).

**b) Debian wrapper (binary swap)** — if you prefer copying static binaries instead
of pulling images:

``` yaml
services:
  axon:
    image: debian:stable-slim
    restart: unless-stopped
    command: ["/bin/axon", "serve", "--config", "/etc/ntfy/server.yml"]
    volumes:
      - ./axon:/bin/axon:ro                 # static binary
      - ./server.yml:/etc/ntfy/server.yml:ro
      - ./poolside.key:/var/lib/ntfy/poolside.key:ro   # omit if no AI
      - ./data:/var/lib/ntfy
    networks:
      - caddy_net

networks:
  caddy_net:
    external: true
    name: <the network your Caddy container is on>
```

Bring it up and verify:

``` bash
docker compose up -d
docker compose logs -f --tail 20 axon
curl -s http://127.0.0.1:2587/v1/health     # {"healthy":true} from the host
```

## 6b. Migrating from a hand-run `docker run` container

If the container was started manually with `docker run` (as in a quick first deploy),
migrate to compose so the config is declarative:

``` bash
docker rm -f axon                      # stop & remove the hand-run container (data is on the bind mounts)
cd /opt/axon && docker compose up -d   # compose recreates it with identical mounts
```

`docker compose logs -f axon` — confirm it comes up healthy; data (cache, user DB) is
untouched because the bind mounts are the same paths.

## 7. Caddy

Add to the Caddyfile that already terminates TLS for your other sites:

``` caddy
axon.example.com {
    reverse_proxy axon:2587          # container DNS name on the shared network
    header {
        X-Content-Type-Options nosniff
        X-Frame-Options DENY
        Referrer-Policy no-referrer
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
    }
}
```

Reload: `docker exec <caddy-container> caddy reload --config /etc/caddy/Caddyfile`
(validate first with `caddy validate`). Certificate is issued automatically.

## 8. Post-install

``` bash
# Admin account (deny-all: the first user must be admin)
docker exec -e NTFY_PASSWORD='<strong-password>' axon \
  /bin/axon user add --role=admin <yourname>

# Agent token (scoped; needed for /mcp tools that touch your account)
docker exec axon /bin/axon --config=/etc/ntfy/server.yml token add --role=admin <yourname>

# Web push (optional)
docker exec axon /bin/axon webpush keys    # paste keys into server.yml, restart
```

Then:

1. Open `https://axon.example.com`, sign in.
2. "Subscribe to topic" — AI dialogs appear in the Subscribe flow and each topic's menu.
3. Settings → **Daily briefing (AI)** — enable, pick hour/timezone.

## 9. Upgrades

``` bash
docker compose pull 2>/dev/null || true   # if you switch to a registry image
docker compose down && docker compose up -d
docker image prune -f
```

- Data lives in the bind mounts — nothing to migrate.
- Image variant (a): after pulling a new image, confirm the `ntfy` uid/gid hasn't
  changed (`docker run --rm ghcr.io/mptfire/axon:latest id`) and re-`chown` `./data`
  if needed. Upgrading from variant (b) to (a): `chown -R 100:101 ./data` once.
- Config changes: edit `server.yml`, `docker compose restart axon`.
- Watch release notes for `server.yml` deprecations before upgrading across minors.

## 10. Backups

Everything stateful is in `/opt/axon/data`:

| File | What | Method |
|---|---|---|
| `user.db` | accounts, tokens, grants | SQLite — stop axon or use `sqlite3 .backup` |
| `cache.db` | message cache | regenerable; back up only if you need message history |
| `attachments/` | uploaded files | rsync |

Nightly cron example:

``` cron
0 4 * * * cd /opt/axon && sqlite3 data/user.db ".backup /backups/user-$(date +\%F).db" && tar czf /backups/axon-$(date +\%F).tgz data/attachments server.yml
```

## 11. Security checklist

- [ ] `auth-default-access: deny-all`
- [ ] `enable-signup: false` once your accounts exist
- [ ] Poolside key file `chmod 600`, owned by the container user, never committed
- [ ] Firewall: only 22/80/443 open; SSH key-only auth; fail2ban for sshd
- [ ] `enable-mcp: true` is safe to expose — every tool call uses the caller's own
      credentials and counts against their budgets; without a token nothing is writable
- [ ] Automatic security updates enabled (`unattended-upgrades`)
- [ ] Caddy admin endpoint not exposed publicly (default: off)

## 12. Troubleshooting

| Symptom | Check |
|---|---|
| Container restart-looping, `exec: no such file` | Binary is dynamically linked — use the static release build |
| `/mcp` returns 403 | Deny-all server: tools need a token; check the caller's Authorization header |
| AI endpoints 500 | `ai-base-url`/model wrong, or provider down — check `docker logs` |
| AI endpoints 429 | Daily token budget exhausted — raise budgets or wait for UTC midnight reset |
| Web shows old UI after upgrade | Service-worker cache; hard refresh, or bump and let the SW update |
