import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";
import { clearUserSession } from "../lib/api.ts";

const source = ts.createSourceFile("auth.ts", readFileSync(new URL("./auth.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const code = source.statements.filter(node => !ts.isImportDeclaration(node)).map(node => node.getText(source).replace(/^export /, "")).join("\n");

function auth(t, storage) {
  const descriptors = new Map(["window", "localStorage"].map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
  Object.defineProperty(globalThis, "window", { configurable: true, value: {} });
  Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
  t.after(() => { for (const [key, descriptor] of descriptors) { if (descriptor) Object.defineProperty(globalThis, key, descriptor); else delete globalThis[key]; } });
  const requests = [];
  const context = {
    API_URL: "https://api.test", clearUserSession, localStorage: storage,
    fetch: async (url, options) => { requests.push({ url, ...options }); },
    create: initializer => {
      let state;
      state = initializer(next => { state = { ...state, ...next }; });
      return { getState: () => state };
    },
  };
  vm.runInNewContext(ts.transpileModule(code + "\nglobalThis.auth = useAuthStore;", { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  return { store: context.auth, requests };
}

test("blocked storage preserves cookie login and in-memory logout; hydration is safe", t => {
  const storage = Object.fromEntries(["getItem", "setItem", "removeItem"].map(key => [key, () => { throw new Error("storage disabled"); }]));
  const { store, requests } = auth(t, storage);
  assert.doesNotThrow(() => store.getState().hydrate());
  const user = { id: 1, email: "fixture@example.test" };
  store.getState().setAuth("unused-cookie-login-token", user);
  assert.equal(store.getState().token, "session");
  assert.equal(store.getState().user, user);
  store.getState().hydrate();
  assert.equal(store.getState().user, user, "blocked hydration must not erase the active in-memory session");
  store.getState().logout();
  assert.equal(store.getState().token, null);
  assert.equal(store.getState().user, null);
  assert.equal(requests[0].credentials, "include");
  assert.equal(requests[0].method, "POST");
});

test("valid cached sessions hydrate and malformed user data is cleared", t => {
  const values = new Map([["starai_session", "1"], ["user", JSON.stringify({ id: 1 })]]);
  const storage = { getItem: key => values.get(key) || null, setItem: (key, value) => values.set(key, value), removeItem: key => values.delete(key) };
  const { store } = auth(t, storage);
  store.getState().hydrate();
  assert.equal(store.getState().token, "session");
  assert.equal(store.getState().user.id, 1);
  values.set("user", "broken JSON");
  assert.doesNotThrow(() => store.getState().hydrate());
  assert.equal(values.has("starai_session"), false);
  assert.equal(values.has("user"), false);
});
