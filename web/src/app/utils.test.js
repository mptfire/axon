import { describe, expect, it } from "vitest";
import { THEME } from "./Prefs";
import {
  topicUrl,
  topicUrlWs,
  topicUrlJsonPollWithSince,
  accountUrl,
  accountTokenUrl,
  shortUrl,
  expandUrl,
  expandSecureUrl,
  validUrl,
  validTopic,
  disallowedTopic,
  encodeBase64,
  encodeBase64Url,
  bearerAuth,
  basicAuth,
  withBearerAuth,
  maybeWithAuth,
  splitNoEmpty,
  hashCode,
  formatBytes,
  formatNumber,
  formatPrice,
  formatShortDuration,
  getKebabCaseLangStr,
  darkModeEnabled,
  urlB64ToUint8Array,
} from "./utils";

describe("URL builders", () => {
  it("build topic URLs", () => {
    expect(topicUrl("https://ntfy.sh", "mytopic")).toBe("https://ntfy.sh/mytopic");
    expect(topicUrlJsonPollWithSince("https://ntfy.sh", "mytopic", 123)).toBe("https://ntfy.sh/mytopic/json?poll=1&since=123");
  });

  it("rewrite the scheme for websocket URLs", () => {
    expect(topicUrlWs("https://ntfy.sh", "mytopic")).toBe("wss://ntfy.sh/mytopic/ws");
    expect(topicUrlWs("http://localhost:8080", "mytopic")).toBe("ws://localhost:8080/mytopic/ws");
  });

  it("build account URLs", () => {
    expect(accountUrl("https://ntfy.sh")).toBe("https://ntfy.sh/v1/account");
    expect(accountTokenUrl("https://ntfy.sh")).toBe("https://ntfy.sh/v1/account/token");
  });
});

describe("url helpers", () => {
  it("strip the scheme with shortUrl", () => {
    expect(shortUrl("https://ntfy.sh/mytopic")).toBe("ntfy.sh/mytopic");
  });

  it("expand a bare host to both schemes", () => {
    expect(expandUrl("ntfy.sh")).toEqual(["https://ntfy.sh", "http://ntfy.sh"]);
    expect(expandSecureUrl("ntfy.sh")).toBe("https://ntfy.sh");
  });

  it("validate http(s) URLs", () => {
    expect(validUrl("https://ntfy.sh")).toBeTruthy();
    expect(validUrl("http://ntfy.sh")).toBeTruthy();
    expect(validUrl("ftp://ntfy.sh")).toBeFalsy();
  });
});

describe("topic validation", () => {
  it("rejects disallowed topics (from config.disallowed_topics)", () => {
    expect(disallowedTopic("app")).toBe(true);
    expect(disallowedTopic("mytopic")).toBe(false);
    expect(validTopic("app")).toBe(false);
  });

  it("accepts well-formed topic names and rejects malformed ones", () => {
    expect(validTopic("valid_Topic-123")).toBeTruthy();
    expect(validTopic("bad/slash")).toBeFalsy();
    expect(validTopic("with space")).toBeFalsy();
    expect(validTopic("")).toBeFalsy();
  });
});

describe("base64 + auth headers", () => {
  it("encodes base64 and base64url", () => {
    expect(encodeBase64("hello")).toBe("aGVsbG8=");
    expect(encodeBase64Url("hello")).toBe("aGVsbG8");
  });

  it("builds bearer and basic auth headers", () => {
    expect(bearerAuth("tok")).toBe("Bearer tok");
    expect(basicAuth("phil", "secret")).toBe(`Basic ${encodeBase64("phil:secret")}`);
    expect(withBearerAuth({ "Content-Type": "application/json" }, "tok")).toEqual({
      "Content-Type": "application/json",
      Authorization: "Bearer tok",
    });
  });

  it("picks the right scheme in maybeWithAuth", () => {
    expect(maybeWithAuth({}, { username: "u", password: "p" }).Authorization).toBe(basicAuth("u", "p"));
    expect(maybeWithAuth({}, { token: "tok" }).Authorization).toBe(bearerAuth("tok"));
    expect(maybeWithAuth({ a: 1 }, undefined)).toEqual({ a: 1 });
  });
});

describe("misc pure helpers", () => {
  it("splitNoEmpty trims and drops empty entries", () => {
    expect(splitNoEmpty("a, b, ,c ", ",")).toEqual(["a", "b", "c"]);
    expect(splitNoEmpty("", ",")).toEqual([]);
  });

  it("hashCode is deterministic", () => {
    expect(hashCode("")).toBe(0);
    expect(hashCode("a")).toBe(97);
    expect(hashCode("hello")).toBe(hashCode("hello"));
  });

  it("formatBytes is human readable", () => {
    expect(formatBytes(0)).toBe("0 bytes");
    expect(formatBytes(1024)).toBe("1 KB");
    expect(formatBytes(1536)).toBe("1.5 KB");
  });

  it("formatNumber abbreviates round thousands", () => {
    expect(formatNumber(0)).toBe(0);
    expect(formatNumber(1000)).toBe("1k");
    expect(formatNumber(1500)).toBe((1500).toLocaleString());
  });

  it("formatPrice renders cents as dollars", () => {
    expect(formatPrice(100)).toBe("$1");
    expect(formatPrice(150)).toBe("$1.5");
  });

  it("formatShortDuration picks the largest fitting unit", () => {
    expect(formatShortDuration(60000, "en")).toContain("minute");
    expect(formatShortDuration(3600000, "en")).toContain("hour");
  });

  it("getKebabCaseLangStr normalizes language tags", () => {
    expect(getKebabCaseLangStr("en_US")).toBe("en-US");
    expect(getKebabCaseLangStr(undefined)).toBe("en");
    expect(getKebabCaseLangStr("")).toBe("en");
  });

  it("darkModeEnabled honors the theme preference", () => {
    expect(darkModeEnabled(false, THEME.DARK)).toBe(true);
    expect(darkModeEnabled(true, THEME.LIGHT)).toBe(false);
    expect(darkModeEnabled(true, THEME.SYSTEM)).toBe(true);
    expect(darkModeEnabled(false, THEME.SYSTEM)).toBe(false);
  });

  it("urlB64ToUint8Array decodes web push keys", () => {
    expect(Array.from(urlB64ToUint8Array("AQID"))).toEqual([1, 2, 3]);
  });
});
