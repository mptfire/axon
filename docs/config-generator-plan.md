# Config Generator for ntfy Docs

## Context

The ntfy config page (`docs/config.md`) documents 60+ server configuration options across many sections. Users often struggle to assemble a working config file. An interactive config generator helps users build a `server.yml`, `docker-compose.yml`, or env vars file by answering guided questions, with real-time preview.

## Files Created

| File | Purpose |
|------|---------|
| `docs/static/js/config-generator.js` | All generator logic (vanilla JS, ~300 lines) |
| `docs/static/css/config-generator.css` | All generator styles (~250 lines) |

## Files Modified

| File | Change |
|------|--------|
| `docs/config.md` | Inserted `## Config generator` HTML block at line 137 (before `## Database options`) |
| `mkdocs.yml` | Added `config-generator.js` to `extra_javascript` and `config-generator.css` to `extra_css` |

## UI Layout

Two-panel side-by-side layout:

- **Left panel**: Collapsible accordion sections with form inputs for config options
- **Right panel** (sticky): Tabs for `server.yml` / `docker-compose.yml` / `Environment variables`, with a copy button. Updates in real-time as the user changes options.
- **Responsive**: Stacks vertically on screens < 900px

## Configuration Sections (Left Panel)

### 1. Basic Setup
- `base-url` (text, placeholder: `https://ntfy.example.com`)
- `listen-http` (text, placeholder: `:80`)
- `behind-proxy` (checkbox)

### 2. Database
- Radio: SQLite (default) vs PostgreSQL
- SQLite → `cache-file` (text, placeholder: `/var/cache/ntfy/cache.db`)
- PostgreSQL → `database-url` (text, placeholder: `postgres://user:pass@host:5432/ntfy`)

### 3. Access Control
- "Enable access control" (checkbox) → shows:
  - `auth-file` (text, placeholder: `/var/lib/ntfy/auth.db`) — hidden if PostgreSQL
  - `auth-default-access` (select: `read-write`, `read-only`, `write-only`, `deny-all`)
  - `enable-login` (checkbox)
  - `enable-signup` (checkbox)
  - Provisioned users (repeatable rows): username, password hash, role (admin/user)
  - Provisioned ACLs (repeatable rows): username, topic pattern, permission (rw/ro/wo/deny)
  - Provisioned tokens (repeatable rows): username, token, label

### 4. Attachments
- "Enable attachments" (checkbox) → shows:
  - `attachment-cache-dir` (text, placeholder: `/var/cache/ntfy/attachments`)
  - `attachment-file-size-limit` (text, placeholder: `15M`)
  - `attachment-total-size-limit` (text, placeholder: `5G`)
  - `attachment-expiry-duration` (text, placeholder: `3h`)

### 5. Message Cache
- `cache-duration` (text, placeholder: `12h`)

### 6. Web Push
- "Enable web push" (checkbox) → shows:
  - `web-push-public-key` (text)
  - `web-push-private-key` (text)
  - `web-push-file` (text, placeholder: `/var/lib/ntfy/webpush.db`) — hidden if PostgreSQL
  - `web-push-email-address` (text)

### 7. Email Notifications (Outgoing)
- "Enable email sending" (checkbox) → shows:
  - `smtp-sender-addr` (text, placeholder: `smtp.example.com:587`)
  - `smtp-sender-from` (text, placeholder: `ntfy@example.com`)
  - `smtp-sender-user` (text)
  - `smtp-sender-pass` (text, type=password)

### 8. Email Publishing (Incoming)
- "Enable email publishing" (checkbox) → shows:
  - `smtp-server-listen` (text, placeholder: `:25`)
  - `smtp-server-domain` (text, placeholder: `ntfy.example.com`)
  - `smtp-server-addr-prefix` (text, placeholder: `ntfy-`)

### 9. Upstream Server
- "iOS users will use this server" (checkbox) → sets `upstream-base-url: https://ntfy.sh`

### 10. Monitoring
- "Enable Prometheus metrics" (checkbox) → sets `enable-metrics: true`

## JavaScript Architecture (`config-generator.js`)

Single vanilla JS file, no dependencies. Key structure:

1. **Config definitions array (`CONFIG`)**: Each entry has `key`, `env`, `type`, `def`, `section`. This is the single source of truth for all three output formats.

2. **`collectValues()`**: Reads all form inputs, returns an object of key→value, filtering out empty values. Handles repeatable rows for auth-users/access/tokens. Respects conditional visibility (skips hidden fields).

3. **`generateServerYml(values)`**: Outputs YAML with section comments. Provisioned users/ACLs/tokens rendered as YAML arrays.

4. **`generateDockerCompose(values)`**: Wraps values as env vars in Docker Compose format. Key transformations:
   - Uses `NTFY_` prefixed env var names from config definitions
   - File paths adjusted for Docker via `DOCKER_PATH_MAP`
   - `$` doubled to `$$` in bcrypt hashes (with comment)
   - Standard boilerplate: `services:`, `image:`, `volumes:`, `ports:`, `command: serve`
   - Provisioned users/ACLs/tokens use indexed env vars (e.g., `NTFY_AUTH_USERS_0_USERNAME`)

5. **`generateEnvVars(values)`**: Simple `export NTFY_KEY="value"` format. Single quotes for values containing `$`.

6. **`updateOutput()`**: Called on every input change. Runs the active tab's generator and updates the `<code>` element.

7. **`initGenerator()`**: Called on DOMContentLoaded, no-ops if `#config-generator` is missing. Sets up event listeners for tabs, accordions, repeatable row add/remove, conditional toggles, and special checkboxes (upstream, metrics).

### Conditional visibility
- Database = PostgreSQL → hide `cache-file`, `auth-file`, `web-push-file`; show `database-url`
- Access control unchecked → hide all auth fields
- Each "enable X" checkbox controls visibility of its section's detail fields via `data-toggle` attribute

### Docker Compose path mapping
- `/var/cache/ntfy/cache.db` → `/var/lib/ntfy/cache.db`
- `/var/cache/ntfy/attachments` → `/var/lib/ntfy/attachments`
- `/var/lib/ntfy/*` → unchanged
- Volume: `./:/var/lib/ntfy`

## CSS Architecture (`config-generator.css`)

Key aspects:

- **Flex layout** with `gap: 24px` for the two-panel layout
- **Sticky right panel**: `position: sticky; top: 76px; align-self: flex-start; max-height: calc(100vh - 100px); overflow-y: auto`
- **Dark mode**: Uses `body[data-md-color-scheme="slate"]` selectors (matching existing pattern in `extra.css`)
- **Accent color**: Uses `var(--md-primary-fg-color)` (`#338574`) for focus states, active tabs, and interactive elements
- **Responsive**: `@media (max-width: 900px)` → column layout, static right panel
- **Accordion**: `.cg-section-header` click toggles `.cg-section.open` which shows/hides `.cg-section-body`
- **Tabs**: `.cg-tab.active` gets accent color border-bottom highlight
- **Code output**: Dark background (`#1e1e1e`) with light text, consistent in both light and dark mode

## HTML Block (in config.md)

~250 lines of HTML inserted at line 137. Pure HTML (no markdown inside). Structure:

```
## Config generator
<div id="config-generator">
  <div id="cg-left">
    <!-- 10 accordion sections with form fields -->
  </div>
  <div id="cg-right">
    <!-- Tab bar + code output + copy button -->
  </div>
</div>
```

## Verification

The docs build was verified with `mkdocs build` — completed successfully with no errors. The generated HTML at `server/docs/config/index.html` contains the config-generator elements.

Manual verification checklist:
1. `cd ntfy && mkdocs serve` — open config page in browser
2. Verify all 10 sections expand/collapse
3. Fill in basic setup → check server.yml output updates
4. Switch tabs → verify docker-compose.yml and env vars render correctly
5. Toggle access control → verify auth fields show/hide
6. Add provisioned users/ACLs/tokens → verify repeatable rows work
7. Switch database to PostgreSQL → verify SQLite fields hide
8. Toggle dark mode → verify styles look correct
9. Resize to mobile width → verify column layout
10. Click copy button → verify clipboard content
