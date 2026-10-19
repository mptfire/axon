# Switching AI providers (e.g. Poolside expiry)

The AI provider is a config-only swap. axon speaks the **OpenAI-compatible
protocol** (`/v1/chat/completions`, `/v1/embeddings`) against any base URL,
plus a native **Anthropic** adapter.

## When: Poolside key expires 31 Oct → switch before 1 Nov

## 1. Pick the new endpoint

Works with any OpenAI-compatible API: the new Poolside contract, OpenRouter,
Groq, Together, Mistral, LM Studio / Ollama (`http://localhost:11434/v1`),
vLLM, or OpenAI itself. You need: base URL, API key, model name(s).

## 2. Edit `/opt/axon/server.yml`

``` yaml
ai-provider: openai            # any OpenAI-compatible endpoint
ai-base-url: "https://NEW-ENDPOINT.example.com/v1"
ai-api-key-file: "/var/lib/ntfy/provider.key"
ai-model: "vendor/model-name"
# Optional second model for cheap features (digest/plan); main model used if unset:
# ai-model-enrich: "vendor/small-model"
```

Put the new key in the key file (single line), then:

``` bash
echo -n 'NEW_API_KEY' | sudo tee /opt/axon/provider.key
sudo sed -i 's|poolside.key|provider.key|' /opt/axon/server.yml
docker restart axon
```

## 3. Verify

``` bash
curl -s -u steve:*** https://axon.rtard.de/v1/ai/status   # provider/model/healthy:true
```

Then exercise each feature once: digest, briefing, chat (web or MCP
`ask_history`), and a publish to an enrichment-enabled topic.

## 4. Disclosure

If `ai-consent-required: true`, update the consent text to name the new
provider: `ai-provider-disclosure: "..."` (what the provider receives, its
retention stance). Check `/v1/ai/disclosure` afterwards.

## 5. Rollback

Keep the old key file; reverting is the same edit with the old values and one
restart. Budget counters reset on restart anyway.

## Notes

- `ai-provider: anthropic` uses the native Messages API (different schema
  path, same features).
- `ai-provider: ollama` needs no key and keeps content on-box.
- The mock provider is for tests only.
- MCP tools, consent, budgets and device channels are provider-agnostic —
  nothing else changes.
