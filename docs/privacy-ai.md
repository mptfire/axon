# AI privacy and account consent

AI is off unless the operator sets `ai-enabled: true`. Enabling it on the server
does not grant consent for any account. Each account owner must review the
configured service and explicitly enable AI data sharing before using it.

The web app shows this disclosure before opening an AI assistant. **Settings →
AI privacy** shows current consent and lets the owner revoke it. The disclosure
identifies the provider, its service origin and configured models, including
embeddings. URL credentials, path and query secrets are omitted. Operators should
separately explain their provider's processing and retention terms. Local Ollama
can keep model processing within the operator's environment; cloud providers
receive selected content outside that environment.

## What leaves the server

| Feature | Content sent to the configured service |
| --- | --- |
| Subscription setup/tuning | Your prompt or goal, prior conversation turns, subscription names and filter settings used as context |
| Enrichment/translation | The authenticated consenting publisher's message text, title and topic, plus the translation language |
| Digest/briefing | Selected messages and metadata from subscribed topics the requesting account can read |
| History chat | Your question and conversation history, plus selected readable messages and metadata used as evidence |
| Semantic retrieval/clustering | Text used to embed the question and selected messages when an embeddings model is configured |

History can include content published by other people. Consent belongs to the
account initiating the AI feature; it does not imply every publisher's consent.
Operators must communicate this behavior and apply appropriate topic policies.
Consent does not expand access. Topic ACLs and model-output validation still apply.

Consent covers AI-scoped agents, opted-in inline enrichment and enabled scheduled
briefings. Enrichment requires an authenticated publisher with AI permission and
current consent. Other publishes pass through unchanged. The separate daily
briefing setting still controls scheduling; absent consent, the scheduler skips
the account without marking that day's briefing as delivered.

Revocation blocks new content requests, including response-cache hits. It cannot
recall content already sent, remove the provider's retained data or cancel work
already authorized and in progress. Server caches and stored delivered messages
retain their existing retention behavior. Ask the operator about those terms.

## Provider changes and API clients

Changing the provider, effective service URL or configured models invalidates
existing consent. The owner must review and accept the new configuration.
Rotating the provider API key alone does not require consent again. Configuration
identities are opaque HMAC values backed by a private persistent database key.
Clients must treat them as identifiers, not derive or interpret them.

Use `GET /v1/ai/consent` to read the authenticated account's disclosure. After
presenting it to the owner, submit its `configuration` value with explicit
acceptance. For example, use an unrestricted account token:

```sh
curl -H "Authorization: Bearer $AXON_ACCOUNT_TOKEN" \
  https://axon.example/v1/ai/consent

# Replace the placeholder with the configuration value the owner reviewed.
curl -X POST -H "Authorization: Bearer $AXON_ACCOUNT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"accepted":true,"configuration":"reviewed-configuration-value"}' \
  https://axon.example/v1/ai/consent

curl -X DELETE -H "Authorization: Bearer $AXON_ACCOUNT_TOKEN" \
  https://axon.example/v1/ai/consent
```

Basic authentication also works. Scoped AI agents can read the disclosure and
use AI after their owner consents, but cannot grant or revoke it. Other scoped
tokens do not gain AI access. Browser writes require the server origin or the
operator's explicit trusted CORS origin; wildcard CORS does not authorize writes.
Acceptance requires JSON and the current configuration.

Missing consent returns HTTP 403, code `40302`, before content transfer,
embeddings, cache lookup or AI quota charging. Stale acceptance returns HTTP 409,
code `40909`; refresh the disclosure and ask the owner to accept again. Database
lookup errors fail closed. Status/health and budget reports send no user content
and need no consent. With AI disabled, the consent endpoint is disabled too.

## Upgrade and rollback

User migration `12 → 13` creates separate consent and private metadata tables on
SQLite and PostgreSQL. It grants no existing account consent. Consent is read
from the primary so replica lag cannot restore a revoked grant. Ordinary account
preferences cannot set or restore it. The account primary key covers lookups.

Back up the user database before upgrading. Migrations run transactionally and
are recorded by the existing schema manager. There is no down migration; older
binaries reject version 13. Rollback requires a compatible binary or restoring
the pre-upgrade database, losing intervening writes. Never renumber migrations
already applied to deployed databases.
