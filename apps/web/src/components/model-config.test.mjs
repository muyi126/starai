import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";

test("returning to the workbench reloads model menus and ignores stale or failed refreshes", async () => {
  const source = ts.createSourceFile("AppShell.tsx", readFileSync(new URL("./AppShell.tsx", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  let effect;
  function visit(node) {
    if (ts.isCallExpression(node) && node.expression.getText(source) === "useEffect" && node.arguments[0]?.getText(source).includes("const loadModel =")) effect = node.arguments[0].getText(source);
    ts.forEachChild(node, visit);
  }
  visit(source);
  assert.ok(effect);
  const listeners = new Map();
  const requests = [];
  let current;
  const events = {
    addEventListener: (name, handler) => listeners.set(name, handler),
    removeEventListener: (name, handler) => { if (listeners.get(name) === handler) listeners.delete(name); },
  };
  const request = (path, locale, options) => new Promise((resolve, reject) => requests.push({ path, locale, options, resolve, reject }));
  const context = { activeModelCode: "seedance-2-5-10s", GENERAL_CREATIVE_AGENT_CODE: "agent", locale: "zh-CN", apiForLocaleCached: request, apiForLocale: request, setActiveModel: model => { current = model; }, window: events, document: { ...events, visibilityState: "visible" } };
  vm.runInNewContext(ts.transpileModule(`var cleanup = (${effect})();`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  assert.equal(requests.length, 1);
  listeners.get("focus")();
  listeners.get("visibilitychange")();
  assert.equal(requests.length, 2, "focus and visibility events must share a pending refresh");
  assert.equal(requests[1].path, "/api/models/seedance-2-5-10s");
  assert.equal(requests[1].options.cache, "no-store");
  const fresh = { modes: ["text", "first_frame", "first_last", "image"] };
  requests[1].resolve(fresh);
  await new Promise(resolve => setImmediate(resolve));
  requests[0].resolve({ modes: ["text", "reference"] });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(current, fresh, "an older cached response must not replace the restored frame modes");
  context.document.visibilityState = "hidden";
  listeners.get("visibilitychange")();
  assert.equal(requests.length, 2);
  context.document.visibilityState = "visible";
  listeners.get("visibilitychange")();
  requests[2].reject(new Error("offline"));
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(current, fresh, "a failed background refresh must preserve the active workspace");
  listeners.get("focus")();
  context.cleanup();
  requests[3].resolve({ modes: [] });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(listeners.size, 0);
  assert.equal(current, fresh, "late responses must not update an unmounted or switched model");
});
