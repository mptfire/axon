// @vitest-environment jsdom
/* eslint-env es2021 */
import * as React from "react";
import { act, useEffect } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import aiApi from "../app/AiApi";
import { AIProviderChangedError } from "../app/errors";
import AiConsentGate from "./AiConsentGate";
import AiPrivacySettings from "./AiPrivacySettings";

vi.mock("../app/AiApi", () => ({ default: { consent: vi.fn(), grantConsent: vi.fn(), revokeConsent: vi.fn() } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key, values = {}) => `${key} ${Object.values(values).join(" ")}`.trim() }) }));

const disclosure = {
  consented: false,
  can_manage: true,
  provider: "openai-compatible",
  endpoint: "https://provider.example",
  models: ["model-default", "model-embed"],
  configuration: "configuration-one",
};

let root;
let container;
let featureWork;
const Feature = () => {
  useEffect(() => { featureWork(); }, []);
  return <div>feature-ready</div>;
};
const button = (text) => [...container.querySelectorAll("button")].find((element) => element.textContent.trim() === text);
const renderGate = (active = true) =>
  act(async () => {
    root.render(<AiConsentGate active={active} onClose={vi.fn()}><Feature /></AiConsentGate>);
  });

beforeEach(() => {
  vi.resetAllMocks();
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  featureWork = vi.fn();
  aiApi.consent.mockResolvedValue(disclosure);
});

afterEach(async () => {
  await act(async () => { root.unmount(); });
  container.remove();
});

describe("explicit account consent", () => {
  it("discloses the service/models before mounting or accepting", async () => {
    await renderGate();
    expect(container.textContent).toContain("https://provider.example");
    expect(container.textContent).toContain("model-default, model-embed");
    expect(featureWork).not.toHaveBeenCalled();
    expect(aiApi.grantConsent).not.toHaveBeenCalled();
    aiApi.grantConsent.mockResolvedValue({ ...disclosure, consented: true });
    await act(async () => { button("ai_consent_accept").click(); });
    expect(aiApi.grantConsent).toHaveBeenCalledExactlyOnceWith("configuration-one");
    expect(featureWork).toHaveBeenCalledOnce();
    expect(container.textContent).toContain("feature-ready");
  });

  it("checks current consent before admitting previously consented accounts", async () => {
    aiApi.consent.mockResolvedValue({ ...disclosure, consented: true });
    await renderGate();
    expect(featureWork).toHaveBeenCalledOnce();
    expect(aiApi.grantConsent).not.toHaveBeenCalled();
  });

  it("requires another explicit acceptance on a provider change", async () => {
    await renderGate();
    aiApi.grantConsent.mockRejectedValueOnce(new AIProviderChangedError());
    aiApi.consent.mockResolvedValue({ ...disclosure, endpoint: "https://new-provider.example", configuration: "configuration-two" });
    await act(async () => { button("ai_consent_accept").click(); });
    expect(container.textContent).toContain("https://new-provider.example");
    expect(featureWork).not.toHaveBeenCalled();
    expect(aiApi.grantConsent).toHaveBeenCalledTimes(1);
    aiApi.grantConsent.mockResolvedValue({ ...disclosure, consented: true, configuration: "configuration-two" });
    await act(async () => { button("ai_consent_accept").click(); });
    expect(aiApi.grantConsent).toHaveBeenLastCalledWith("configuration-two");
    expect(featureWork).toHaveBeenCalledOnce();
  });

  it("blocks scoped agents and failed disclosure loads", async () => {
    aiApi.consent.mockResolvedValue({ ...disclosure, can_manage: false });
    await renderGate();
    expect(button("ai_consent_accept").disabled).toBe(true);
    await act(async () => { button("ai_consent_accept").click(); });
    expect(aiApi.grantConsent).not.toHaveBeenCalled();
    await renderGate(false);
    aiApi.consent.mockRejectedValue(new Error("metadata unavailable"));
    await renderGate();
    expect(container.textContent).toContain("metadata unavailable");
    expect(button("ai_consent_accept").disabled).toBe(true);
    expect(featureWork).not.toHaveBeenCalled();
  });

  it("ignores acceptance from a closed, then reopened dialog", async () => {
    let complete;
    aiApi.grantConsent.mockReturnValue(new Promise((resolve) => { complete = resolve; }));
    await renderGate();
    await act(async () => { button("ai_consent_accept").click(); });
    await renderGate(false);
    aiApi.consent.mockResolvedValue({ ...disclosure, configuration: "configuration-two" });
    await renderGate();
    await act(async () => { complete({ ...disclosure, consented: true }); });
    expect(featureWork).not.toHaveBeenCalled();
    expect(button("ai_consent_accept").disabled).toBe(false);
  });

  it("revokes through privacy settings without modifying preferences", async () => {
    aiApi.consent.mockResolvedValue({ ...disclosure, consented: true });
    aiApi.revokeConsent.mockResolvedValue(disclosure);
    await act(async () => { root.render(<AiPrivacySettings />); });
    await act(async () => { button("ai_consent_revoke").click(); });
    expect(aiApi.revokeConsent).toHaveBeenCalledOnce();
    expect(container.textContent).toContain("ai_consent_disabled");
    expect(aiApi.grantConsent).not.toHaveBeenCalled();
  });
});
