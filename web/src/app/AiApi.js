import { maybeWithBearerAuth } from "./utils";
import session from "./Session";
import { fetchOrThrow } from "./errors";

// AiApi talks to the axon AI endpoints (/v1/ai/*). They only exist when the server
// operator enabled the AI layer (config.enable_ai); the UI must hide itself otherwise.
class AiApi {
  async plan(prompt, locale, context = []) {
    const url = `${config.base_url}/v1/ai/plan`;
    console.log(`[AiApi] Planning subscriptions for prompt (${prompt.length} chars, ${context.length} prior turns)`);
    const response = await fetchOrThrow(url, {
      method: "POST",
      headers: maybeWithBearerAuth({ "Content-Type": "application/json" }, session.token()),
      body: JSON.stringify({ prompt, locale, context }), // context: prior user/assistant turns for refinement
    });
    return response.json(); // May throw SyntaxError
  }

  async chat(topic, question, since = "168h") {
    const url = `${config.base_url}/v1/ai/chat`;
    console.log(`[AiApi] Asking about ${topic} (since ${since})`);
    const response = await fetchOrThrow(url, {
      method: "POST",
      headers: maybeWithBearerAuth({ "Content-Type": "application/json" }, session.token()),
      body: JSON.stringify({ topic, question, since }),
    });
    return response.json(); // May throw SyntaxError
  }

  async digest(topic, since) {
    const url = `${config.base_url}/v1/ai/digest`;
    console.log(`[AiApi] Requesting digest of ${topic} (since ${since})`);
    const response = await fetchOrThrow(url, {
      method: "POST",
      headers: maybeWithBearerAuth({ "Content-Type": "application/json" }, session.token()),
      body: JSON.stringify({ topic, since }),
    });
    return response.json(); // May throw SyntaxError
  }

  async tune(topic, { displayName, search, minPriority }, goal) {
    const url = `${config.base_url}/v1/ai/tune`;
    console.log(`[AiApi] Tuning subscription ${topic}`);
    const response = await fetchOrThrow(url, {
      method: "POST",
      headers: maybeWithBearerAuth({ "Content-Type": "application/json" }, session.token()),
      body: JSON.stringify({
        topic,
        display_name: displayName || "",
        search: search || "",
        min_priority: minPriority || 0,
        goal,
      }),
    });
    return response.json(); // May throw SyntaxError
  }
}

export default new AiApi();
