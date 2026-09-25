import { maybeWithBearerAuth } from "./utils";
import session from "./Session";
import { fetchOrThrow, throwAppError } from "./errors";

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

  async briefing(since) {
    const url = `${config.base_url}/v1/ai/briefing`;
    console.log(`[AiApi] Requesting briefing (since ${since})`);
    const response = await fetchOrThrow(url, {
      method: "POST",
      headers: maybeWithBearerAuth({ "Content-Type": "application/json" }, session.token()),
      body: JSON.stringify({ since }),
    });
    return response.json(); // May throw SyntaxError
  }

  async chatStream(topic, question, { history = [], since = "168h", all = false, onDelta, onCitations }) {
    const url = `${config.base_url}/v1/ai/chat/stream`;
    console.log(`[AiApi] Streaming chat about ${all ? "all topics" : topic}`);
    const response = await fetch(url, {
      method: "POST",
      headers: maybeWithBearerAuth({ "Content-Type": "application/json" }, session.token()),
      body: JSON.stringify({ topic, question, since, history, all }),
    });
    if (!response.ok) {
      await throwAppError(response);
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      buffer += decoder.decode(value, { stream: true });
      const frames = buffer.split("\n\n");
      buffer = frames.pop(); // Last frame may be incomplete
      for (const frame of frames) {
        const line = frame.trim();
        if (!line.startsWith("data:")) {
          continue;
        }
        const event = JSON.parse(line.slice(5).trim());
        if (event.type === "delta" && onDelta) {
          onDelta(event.text);
        } else if (event.type === "citations" && onCitations) {
          onCitations(event.text, event.citations ?? []);
        }
      }
    }
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
