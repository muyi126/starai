import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";

const source = ts.createSourceFile("ModelWorkspace.tsx", readFileSync(new URL("./ModelWorkspace.tsx", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
function find(predicate) {
  let result;
  function visit(node) { if (!result && predicate(node)) result = node; if (!result) ts.forEachChild(node, visit); }
  visit(source);
  assert.ok(result);
  return result;
}
function run(code, context) { vm.runInNewContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context); }

test("initial load and config refresh both respect administrator defaults over schema fallbacks", () => {
  const declaration = find(n => ts.isVariableDeclaration(n) && n.name.getText(source) === "[params, setParams]");
  const effect = find(n => ts.isCallExpression(n) && n.expression.getText(source) === "useEffect" && n.arguments[0]?.getText(source).includes("initializedParamsKeyRef.current ="));
  for (const category of ["video", "audio"]) {
    let actual;
    const context = {
      useState: fn => [fn(), value => { actual = value; }], model: { category, default_params: { duration: 5, resolution: "720p" } },
      schemaDefaultsFromFields: () => ({ duration: 4, resolution: "480p", generation_mode: "text" }),
      schemaDefaults: () => ({}), initializedParamsKeyRef: { current: "" }, initializationKey: "new",
      parseAudioRuntime: () => ({}), setParams: value => { actual = value; }, isVideo: category === "video", isAudio: category === "audio", isImage: false,
      workbenchInputSchema: {}, EMPTY_VIDEO_MEDIA: {}, setRefImages() {}, setVideoMedia() {}, setAudioSecondaryPrompt() {}, setAudioRef() {}, setBottom() {}, setDeepThink() {}, setPrompt() {}, capDeepThink: false, initialPrompt: "",
    };
    run(`var ${declaration.getText(source)}; var initial = params; (${effect.arguments[0].getText(source)})();`, context);
    for (const values of [context.initial, actual]) {
      assert.equal(values.duration, 5);
      assert.equal(values.resolution, "720p");
      assert.equal(values.generation_mode, "text");
    }
  }
});

test("quotes debounce rapid edits and discard late responses after tier switches or unmount", async () => {
  const effect = find(n => ts.isCallExpression(n) && n.expression.getText(source) === "useEffect" && n.arguments[0]?.getText(source).includes("const controller = new AbortController"));
  const timers = new Map(), requests = [];
  let id = 0, quote;
  const context = {
    AbortController, model: { code: "minimax-h3" }, estimateBody: '{"params":{"resolution":"480p"}}',
    window: { setTimeout: fn => { timers.set(++id, fn); return id; }, clearTimeout: key => timers.delete(key) },
    setEstimatedCost: value => { quote = value; }, setEstimateError() {}, isMultiCollab: false,
    api: (path, options) => new Promise((resolve, reject) => requests.push({ path, options, resolve, reject })),
  };
  const start = () => { run(`var cleanup = (${effect.arguments[0].getText(source)})();`, context); return context.cleanup; };
  const flush = () => { for (const [key, fn] of timers) { timers.delete(key); fn(); } };
  const tick = () => new Promise(resolve => setImmediate(resolve));
  for (let edit = 0; edit < 5; edit++) start()();
  assert.equal(timers.size, 0);
  let cleanup = start(); flush();
  assert.equal(requests.length, 1, "canceled edits must not send any requests");
  cleanup();
  assert.equal(requests[0].options.signal.aborted, true);
  context.estimateBody = '{"params":{"resolution":"768p"}}';
  cleanup = start(); flush();
  requests[1].resolve({ estimated_cost: 0.4 }); await tick();
  assert.equal(quote, 0.4);
  requests[0].resolve({ estimated_cost: 0.3 }); await tick();
  assert.equal(quote, 0.4, "old resolution quote must not replace the current one");
  cleanup(); cleanup = start(); flush(); cleanup();
  requests[2].resolve({ estimated_cost: 10 }); await tick();
  assert.equal(quote, null, "unmounted component must ignore a late quote");
});
