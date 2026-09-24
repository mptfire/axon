import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import session from "./Session";
import aiApi from "./AiApi";

// AiApi posts to the axon AI endpoints with the session token (if any) attached.
// The real Session singleton is used; token() is spied per-test.

let fetchMock;

beforeEach(() => {
  vi.clearAllMocks();
  vi.spyOn(console, "log").mockImplementation(() => {});
  fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
  vi.spyOn(session, "token").mockReturnValue(undefined); // anonymous by default
  config.base_url = "https://axon.example.com";
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("AiApi.plan", () => {
  it("posts the prompt to /v1/ai/plan and returns the plan", async () => {
    const plan = { base_url: "https://axon.example.com", subscriptions: [{ topic: "phil-ci" }], disclaimer: "d" };
    fetchMock.mockResolvedValue({ status: 200, json: async () => plan });

    const result = await aiApi.plan("notify me when CI fails", "en");

    expect(result).toEqual(plan);
    expect(fetchMock).toHaveBeenCalledWith("https://axon.example.com/v1/ai/plan", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ prompt: "notify me when CI fails", locale: "en", context: [] }),
    });
  });

  it("passes prior turns as refinement context", async () => {
    fetchMock.mockResolvedValue({ status: 200, json: async () => ({}) });
    const context = [
      { role: "user", content: "notify me about CI" },
      { role: "assistant", content: "{plan}" },
    ];
    await aiApi.plan("less noise", "en", context);
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
      prompt: "less noise",
      locale: "en",
      context,
    });
  });

  it("attaches the bearer token when logged in", async () => {
    session.token.mockReturnValue("tk_123"); // already a spy from beforeEach
    fetchMock.mockResolvedValue({ status: 200, json: async () => ({}) });

    await aiApi.plan("wish", "en");

    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe("Bearer tk_123");
  });

  it("throws on rate-limit responses", async () => {
    fetchMock.mockResolvedValue({
      status: 429,
      json: async () => ({ code: 42912, http: 429, error: "limit reached: ai token budget exceeded" }),
    });

    await expect(aiApi.plan("wish", "en")).rejects.toThrow("limit reached");
  });
});

describe("AiApi.tune", () => {
  it("posts current settings and the goal to /v1/ai/tune", async () => {
    const tune = { display_name: "Night pages", filters: { min_priority: 4 } };
    fetchMock.mockResolvedValue({ status: 200, json: async () => tune });

    const result = await aiApi.tune("prod-alerts", { displayName: "Prod", search: "old", minPriority: 2 }, "quieter");

    expect(result).toEqual(tune);
    expect(fetchMock).toHaveBeenCalledWith("https://axon.example.com/v1/ai/tune", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ topic: "prod-alerts", display_name: "Prod", search: "old", min_priority: 2, goal: "quieter" }),
    });
  });
});
