import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
import * as translations from "./translation.ts";
import * as builtins from "./builtins.ts";
import en from "./locales/en-US.ts";
import ja from "./locales/ja-JP.ts";
import ko from "./locales/ko-KR.ts";
import vi from "./locales/vi-VN.ts";

const source = ts.createSourceFile("provider.tsx", readFileSync(new URL("./I18nProvider.tsx", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const code = source.statements.filter(ts.isFunctionDeclaration).map((node) => node.getText(source).replace(/^export /, "")).join("\n");
const flush = () => new Promise((resolve) => setImmediate(resolve));
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };

// Run the real provider with controlled dictionary/config timing and tiny hook stubs.
function provider({ config = Promise.resolve({ default_locale: "en-US" }), load = async () => {}, user = null, overrides = () => Promise.resolve([]), stored = "", blockedStorage = false } = {}) {
  const state = [], memo = [], refs = [], effects = [];
  let cursor = 0, scheduled = [];
  const storage = new Map(stored ? [["site_locale", stored]] : []), events = [], patches = [];
  const auth = { user, hydrate() { if (blockedStorage) throw new Error("storage disabled"); } };
  const store = (select) => select(auth);
  store.getState = () => auth;
  const memoize = (fn, deps) => {
    const index = cursor++;
    const old = memo[index];
    if (!old || deps.some((value, i) => !Object.is(value, old.deps[i]))) memo[index] = { deps, value: fn() };
    return memo[index].value;
  };
  const ctx = {
    ...translations, ...builtins, console,
    DEFAULT_UI_LANGUAGES: ["zh-CN", "en-US", "ja-JP", "ko-KR", "vi-VN"].map((code) => ({ code, short: code, name: code, enabled: true })),
    SUPPORTED_UI_LOCALES: ["zh-CN", "en-US", "ja-JP", "ko-KR", "vi-VN"],
    dictionaries: { "zh-CN": {}, "en-US": {} }, sourceTranslations: {},
    I18nContext: { Provider: "provider" },
    React: { createElement: (_, props) => props.value },
    useAuthStore: store, loadLocaleDictionaries: load,
    apiCached: (path) => path.startsWith("/api/ui-translations") ? overrides(path) : config, api: async (_, options) => { patches.push(JSON.parse(options.body)); return {}; },
    hasUserSession: () => true,
    useState: (initial) => { const index = cursor++; if (!(index in state)) state[index] = initial; return [state[index], (value) => { state[index] = typeof value === "function" ? value(state[index]) : value; }]; },
    useRef: (value) => { const index = cursor++; return refs[index] ||= { current: value }; },
    useCallback: (fn, deps) => memoize(() => fn, deps), useMemo: memoize,
    useEffect: (fn, deps) => {
      const index = cursor++, old = effects[index];
      if (!old || deps.some((value, i) => !Object.is(value, old.deps[i]))) {
        scheduled.push(() => { old?.cleanup?.(); effects[index] = { deps, cleanup: fn() }; });
      }
    },
    localStorage: { getItem: (key) => { if (blockedStorage) throw new Error("storage disabled"); return storage.get(key) || null; }, setItem: (key, value) => { if (blockedStorage) throw new Error("storage disabled"); storage.set(key, value); } },
    navigator: { language: "zh-CN" }, document: { documentElement: {} },
    window: { dispatchEvent: (event) => events.push(event) },
    CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options.detail; } },
  };
  vm.runInNewContext(ts.transpileModule(code, { compilerOptions: { jsx: ts.JsxEmit.React, target: ts.ScriptTarget.ES2022 } }).outputText, ctx);
  const render = () => { cursor = 0; scheduled = []; const value = ctx.I18nProvider({ children: null }); scheduled.forEach((fn) => fn()); return value; };
  return { render, storage, events, patches, ctx };
}

test("default and profile locale initialization also set the language used by API requests", async () => {
  for (const user of [null, { locale: "ja-JP" }]) {
    const app = provider({ user });
    app.render();
    await flush();
    const current = app.render();
    const expected = user?.locale || "en-US";
    assert.equal(current.locale, expected);
    assert.equal(app.storage.get("site_locale"), expected);
    assert.equal(app.ctx.document.documentElement.lang, expected);
  }
});

test("rapid language changes commit only the latest loaded dictionary", async () => {
  const requests = new Map(["ja-JP", "ko-KR"].map((locale) => [locale, deferred()]));
  const app = provider({ load: (locale) => requests.get(locale)?.promise || Promise.resolve() });
  app.render(); await flush();
  const current = app.render();
  current.setLocale("ja-JP"); current.setLocale("ko-KR");
  assert.equal(app.storage.get("site_locale"), "en-US", "headers stay aligned with the visible UI while loading");
  requests.get("ko-KR").resolve(); await flush();
  requests.get("ja-JP").resolve(); await flush();
  assert.equal(app.render().locale, "ko-KR");
  assert.equal(app.storage.get("site_locale"), "ko-KR");
  assert.deepEqual(app.patches, [{ locale: "ko-KR" }]);
  assert.equal(app.events.length, 1);
});

test("late configuration cannot override a manual selection; failed dictionary preserves the last usable language", async () => {
  const config = deferred(), dictionary = deferred();
  const app = provider({ config: config.promise, load: (locale) => locale === "ja-JP" ? dictionary.promise : Promise.resolve() });
  app.render().setLocale("ja-JP");
  config.resolve({ default_locale: "en-US" }); await flush();
  dictionary.resolve(); await flush();
  assert.equal(app.render().locale, "ja-JP");
  const failed = provider({ load: (locale) => locale === "ko-KR" ? Promise.reject(new Error("chunk unavailable")) : Promise.resolve() });
  failed.render(); await flush();
  failed.render().setLocale("ko-KR"); await flush();
  assert.equal(failed.render().locale, "en-US");
  assert.equal(failed.storage.get("site_locale"), "en-US");
  assert.deepEqual(failed.patches, []);
});

test("saved locale loads before slow public config and override requests finish", async () => {
  const config = deferred(), overrides = deferred(), dictionary = deferred();
  const loads = [];
  const app = provider({ stored: "ja-JP", config: config.promise, overrides: () => overrides.promise, load: (locale) => { loads.push(locale); return dictionary.promise; } });
  assert.equal(app.render().locale, "zh-CN", "server and first client render still agree");
  assert.deepEqual(loads, ["ja-JP"]);
  dictionary.resolve(); await flush();
  assert.equal(app.render().locale, "ja-JP", "network settings are not a prerequisite to a ready dictionary");
  config.resolve({ ui_languages: [{ code: "en-US", enabled: true }] }); await flush();
  assert.equal(app.render().locale, "en-US", "later public config still enforces enabled languages");
});

test("override requests never block switching, stale replies cannot win, and removed overrides disappear", async () => {
  const ja = deferred(), ko = deferred();
  let removeKo = false;
  const app = provider({ overrides: (path) => path.endsWith("ja-JP") ? ja.promise : path.endsWith("ko-KR") ? removeKo ? Promise.resolve([]) : ko.promise : Promise.resolve([]) });
  app.render(); await flush();
  app.render().setLocale("ja-JP"); await flush();
  assert.equal(app.render().locale, "ja-JP");
  app.render().setLocale("ko-KR"); await flush();
  ko.resolve([{ locale: "ko-KR", key: "common.loading", value: "Custom Korean" }]); await flush();
  ja.resolve([{ locale: "ja-JP", key: "common.loading", value: "Stale Japanese" }]); await flush();
  assert.equal(app.render().locale, "ko-KR");
  assert.equal(app.render().t("common.loading"), "Custom Korean");
  removeKo = true;
  app.render().setLocale("ko-KR"); await flush();
  assert.equal(app.render().t("common.loading"), builtins.BUILTIN_KEY_TRANSLATIONS["common.loading"]["ko-KR"]);
});

test("blocked browser storage cannot prevent language activation or produce an unhandled rejection", async () => {
  const app = provider({ blockedStorage: true });
  app.render(); await flush();
  assert.equal(app.render().locale, "en-US");
  assert.equal(app.ctx.document.documentElement.lang, "en-US");
  app.render().setLocale("ja-JP"); await flush();
  assert.equal(app.render().locale, "ja-JP");
});

test("translated keys do not build or scan the Chinese source fallback index", async () => {
  const app = provider();
  let lookups = 0;
  app.ctx.translateBuiltinSource = () => { lookups += 1; return ""; };
  app.render(); await flush();
  const current = app.render();
  for (let index = 0; index < 1000; index++) assert.equal(current.t("common.loading"), "Loading...");
  assert.equal(lookups, 0);
  current.t("missing.key");
  assert.equal(lookups, 1);
});

test("a selected Japanese, Korean or Vietnamese locale renders actual localized landing and language controls", async () => {
  for (const [locale, dictionary] of [["ja-JP", ja], ["ko-KR", ko], ["vi-VN", vi]]) {
    const app = provider({ stored: locale });
    app.ctx.dictionaries = { "zh-CN": {}, "en-US": en, [locale]: dictionary };
    app.render(); await flush();
    const current = app.render();
    assert.equal(current.locale, locale);
    for (const key of ["common.language", "landing.cta", "landing.capability.media.desc", "landing.section.capability", "login.title", "nav.workspace", "asset.uploadAsset", "workspace.generationProgress", "agent.helpDefault", "menu.quickEntry", "channel.price_first.desc"]) {
      assert.equal(current.t(key), dictionary[key]);
      assert.notEqual(current.t(key), en[key]);
    }
  }
});
