import * as React from "react";
import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter, Route, Routes, useLocation, useParams } from "react-router-dom";
import routes from "./routes";

vi.mock("../app/config", () => ({ default: { app_root: "/app", base_url: "https://axon.example" } }));

const Page = ({ name }) => {
  const params = useParams();
  const location = useLocation();
  return (
    <output data-route={name} data-topic={params.topic} data-base-url={params.baseUrl} data-token={params.token} data-search={location.search} />
  );
};

const renderRoute = (path) =>
  renderToStaticMarkup(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        {Object.entries(routes)
          .filter(([, pattern]) => typeof pattern === "string")
          .map(([name, pattern]) => (
            <Route key={name} path={pattern} element={<Page name={name} />} />
          ))}
      </Routes>
    </MemoryRouter>,
  );

describe("declarative app routes", () => {
  it.each(["app", "login", "signup", "account", "settings", "passwordResetRequest"])(
    "keeps %s ahead of the topic route",
    (name) => {
      const html = renderRoute(routes[name]);
      expect(html).toContain(`data-route="${name}"`);
      expect(html).not.toContain("data-topic=");
    },
  );

  it.each(["passwordReset", "emailVerify"])("preserves the %s token parameter", (name) => {
    const html = renderRoute(routes[name].replace(":token", "tk_abc123"));
    expect(html).toContain(`data-route="${name}"`);
    expect(html).toContain('data-token="tk_abc123"');
  });

  it("navigates to a local subscription and retains notification query parameters", () => {
    const path = routes.forSubscription({ baseUrl: "https://axon.example", topic: "prod-alerts" });
    expect(path).toBe("/prod-alerts");
    const html = renderRoute(`${path}?id=abc123`);
    expect(html).toContain('data-route="subscription"');
    expect(html).toContain('data-topic="prod-alerts"');
    expect(html).toContain('data-search="?id=abc123"');
  });

  it("navigates to an external subscription, including a custom port", () => {
    const path = routes.forSubscription({ baseUrl: "https://ntfy.example:8443", topic: "backups" });
    expect(path).toBe("/ntfy.example:8443/backups");
    const html = renderRoute(path);
    expect(html).toContain('data-route="subscriptionExternal"');
    expect(html).toContain('data-base-url="ntfy.example:8443"');
    expect(html).toContain('data-topic="backups"');
  });
});
