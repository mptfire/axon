import { describe, expect, it, vi } from "vitest";

vi.mock("./Api", () => ({ default: {} }));
vi.mock("./Notifier", () => ({ default: { notify: vi.fn() } }));
vi.mock("./Prefs", () => ({ default: { minPriority: vi.fn().mockResolvedValue(1) } }));
vi.mock("./db", () => ({ default: () => ({}) }));
const { SubscriptionManager } = await import("./SubscriptionManager");
const { default: notifier } = await import("./Notifier");
const { default: prefs } = await import("./Prefs");

describe("subscription priority", () => {
  it("does not notify below the subscription minimum", async () => {
    notifier.notify.mockClear();
    const manager = new SubscriptionManager({ subscriptions: { get: async () => ({ minPriority: 4, mutedUntil: 0 }) } });
    await manager.notify("alerts", { event: "message", priority: 3 });
    expect(notifier.notify).not.toHaveBeenCalled();
    await manager.notify("alerts", { event: "message", priority: 4 });
    expect(notifier.notify).toHaveBeenCalledTimes(1);
  });

  it("keeps the global minimum when it is stricter", async () => {
    notifier.notify.mockClear();
    prefs.minPriority.mockResolvedValueOnce(5);
    const manager = new SubscriptionManager({ subscriptions: { get: async () => ({ minPriority: 2, mutedUntil: 0 }) } });
    await manager.notify("alerts", { event: "message", priority: 4 });
    expect(notifier.notify).not.toHaveBeenCalled();
  });

  it("keeps existing notifications when no subscription minimum is set", async () => {
    notifier.notify.mockClear();
    const manager = new SubscriptionManager({ subscriptions: { get: async () => ({ mutedUntil: 0 }) } });
    await manager.notify("alerts", { event: "message" });
    expect(notifier.notify).toHaveBeenCalledTimes(1);
  });
});
