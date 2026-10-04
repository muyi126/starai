import assert from "node:assert/strict";
import test from "node:test";
import { api, apiCached, apiForLocaleCached, clearApiCache, listChannelPresets, hasUserSession, legacyAuthHeaders } from "./api.ts";

const response = (data) => new Response(JSON.stringify({ code: 0, data }));

test("expired cookie redirects and invalidates metadata even when storage removal is blocked", async t => {
  clearApiCache();
  const redirects = [];
  const restore = [];
  for (const [name, value] of Object.entries({
    window: { location: { pathname: "/app", replace: url => redirects.push(url) } },
    localStorage: { getItem: () => { throw new Error("disabled"); }, removeItem: () => { throw new Error("disabled"); } },
  })) {
    restore.push([name, Object.getOwnPropertyDescriptor(globalThis, name)]);
    Object.defineProperty(globalThis, name, { configurable: true, value });
  }
  t.after(() => { for (const [name, descriptor] of restore) descriptor ? Object.defineProperty(globalThis, name, descriptor) : delete globalThis[name]; });
  let calls = 0;
  t.mock.method(globalThis, "fetch", async url => {
    calls++;
    return url.endsWith("/api/me") ? new Response(JSON.stringify({ message: "expired" }), { status: 401 }) : response(calls);
  });
  assert.equal(await apiCached("/cached"), 1);
  await assert.rejects(api("/api/me"), /expired/);
  assert.deepEqual(redirects, ["/?login=1&reason=expired"]);
  assert.equal(await apiCached("/cached"), 3, "expired session cache must be cleared");
});

test("blocked storage keeps public requests and locale-isolated cache usable without adding credentials", async (t) => {
  clearApiCache();
  const restore = [];
  for (const [name, value] of Object.entries({ window: {}, document: { documentElement: { lang: "ja-JP" } }, localStorage: { getItem: () => { throw new Error("disabled"); } } })) {
    restore.push([name, Object.getOwnPropertyDescriptor(globalThis, name)]);
    Object.defineProperty(globalThis, name, { configurable: true, value });
  }
  t.after(() => { for (const [name, descriptor] of restore) descriptor ? Object.defineProperty(globalThis, name, descriptor) : delete globalThis[name]; });
  const requests = [];
  t.mock.method(globalThis, "fetch", async (_, options) => { requests.push(options); return response(requests.length); });
  assert.equal(hasUserSession(), false);
  assert.deepEqual(legacyAuthHeaders(), {});
  assert.equal(await apiCached("/models"), 1);
  assert.equal(await apiCached("/models"), 1);
  globalThis.document.documentElement.lang = "ko-KR";
  assert.equal(await apiCached("/models"), 2);
  assert.equal(requests[0].headers["X-Locale"], "ja-JP");
  assert.equal(requests[1].headers["Accept-Language"], "ko-KR");
  assert.equal(requests[0].headers.Authorization, undefined);
  assert.equal(requests[0].credentials, "include");
  await api("/public");
});

test("chat workspace and channel toolbar reuse the same presets request", async (t) => {
  clearApiCache();
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => {
    calls++;
    return response({ items: [{ key: "price_first" }] });
  });
  const [workspace, toolbar] = await Promise.all([apiCached("/api/channel-presets"), listChannelPresets()]);
  assert.deepEqual(workspace, toolbar);
  assert.equal(calls, 1);
});

test("metadata reads coalesce, isolate locales and retry failures", async (t) => {
  clearApiCache();
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => response(++calls));
  const values = await Promise.all([apiForLocaleCached("/models", "zh-CN"), apiForLocaleCached("/models", "zh-CN")]);
  assert.deepEqual(values, [1, 1]);
  assert.equal(await apiForLocaleCached("/models", "en-US"), 2);
  globalThis.fetch.mock.mockImplementationOnce(async () => { throw new Error("offline"); });
  await assert.rejects(apiCached("/retry"), /offline/);
  assert.equal(await apiCached("/retry"), 3);
});

test("a failed request from before cache invalidation cannot evict the new response", async (t) => {
  clearApiCache();
  let rejectOld;
  let calls = 0;
  t.mock.method(globalThis, "fetch", () => {
    calls++;
    return calls === 1 ? new Promise((_, reject) => { rejectOld = reject; }) : Promise.resolve(response("new"));
  });
  const old = apiCached("/same");
  const rejected = assert.rejects(old, /old failure/);
  clearApiCache();
  assert.equal(await apiCached("/same"), "new");
  rejectOld(new Error("old failure"));
  await rejected;
  assert.equal(await apiCached("/same"), "new");
  assert.equal(calls, 2);
});

test("metadata cache expires entries and caps retained responses", async (t) => {
  clearApiCache();
  let now = 1000;
  let calls = 0;
  t.mock.method(Date, "now", () => now);
  t.mock.method(globalThis, "fetch", async () => response(++calls));
  assert.equal(await apiCached("/expires", 10), 1);
  now += 11;
  assert.equal(await apiCached("/expires", 10), 2);
  clearApiCache();
  for (let i = 0; i < 129; i++) await apiCached(`/model/${i}`);
  const previous = calls;
  await apiCached("/model/0");
  assert.equal(calls, previous + 1);
  await apiCached("/model/128");
  assert.equal(calls, previous + 1);
});
