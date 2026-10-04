import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";

function sourceFile(path) {
  return ts.createSourceFile(path, readFileSync(new URL(path, import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
}
function findNode(source, predicate) {
  let found;
  function visit(node) { if (!found && predicate(node)) found = node; if (!found) ts.forEachChild(node, visit); }
  visit(source);
  assert.ok(found);
  return found;
}
function execute(text, context) {
  vm.runInNewContext(ts.transpileModule(text, { compilerOptions: { target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.React, module: ts.ModuleKind.CommonJS } }).outputText, context);
}
function elements(node) {
  if (Array.isArray(node)) return node.flatMap(elements);
  if (!node || typeof node !== "object") return [];
  return [node, ...elements(node.props?.children)];
}
const React = { Fragment: "fragment", createElement: (type, props, ...children) => ({ type, props: { ...props, children } }) };

test("real numeric schema controls allow decimals, omit cleared optional seeds and retain integer errors for validation", () => {
  const source = sourceFile("SchemaForm.tsx");
  const context = { exports: {}, React, useI18n: () => ({ ts: (value) => value }) };
  execute(source.statements.filter((node) => !ts.isImportDeclaration(node)).map((node) => node.getText(source)).join("\n"), context);
  for (const [type, step] of [["number", "any"], ["integer", 1]]) {
    const changes = [];
    const tree = context.SchemaForm({ schema: { properties: { seed: { type, title: "种子" } } }, values: {}, onChange: (next) => changes.push(next) });
    const input = elements(tree).find((node) => node.type === "input");
    assert.equal(input.props.step, step);
    assert.ok(input.props.className.includes("dark:"));
    input.props.onChange({ target: { value: "" } });
    assert.equal(changes[0].seed, undefined);
    assert.equal(JSON.stringify(changes[0]), "{}");
    input.props.onChange({ target: { value: "2.5" } });
    assert.equal(changes[1].seed, 2.5);
  }
});

test("actual image count menu supports MiniMax 1-9 and obeys custom-count restrictions", () => {
  const source = sourceFile("ImageGenerationToolbar.tsx");
  const changes = [];
  const context = { exports: {}, React, useMemo: (fn) => fn(), useState: () => ["9", () => {}], useI18n: () => ({ t: (key) => key, ts: (key) => key }), Grid3X3: "icon", FileText: "icon", Settings: "icon", MediaOptionMenu: "menu", MediaMenuOption: "option" };
  execute(source.statements.filter((node) => !ts.isImportDeclaration(node)).map((node) => node.getText(source)).join("\n"), context);
  const props = { count: 1, onCountChange: (n) => changes.push(n), ratio: "1:1", onRatioChange: () => {}, imageSize: "1K", onImageSizeChange: () => {}, countOptions: [1, 2, 3, 4], countMax: 9, showSizeTier: false };
  const countMenu = elements(context.ImageGenerationToolbar(props)).find((node) => node.type === "menu");
  const menu = elements(countMenu.props.children[0](() => {}));
  assert.equal(menu.find((node) => node.type === "input").props.max, 9);
  menu.find((node) => node.type === "button").props.onClick();
  assert.deepEqual(changes, [9]);
  assert.equal(context.buildImageGenerationParams({ count: 9, ratio: "1:1" }).n, 9);
  const restricted = elements(context.ImageGenerationToolbar({ ...props, countAllowCustom: false })).find((node) => node.type === "menu");
  assert.equal(elements(restricted.props.children[0](() => {})).some((node) => node.type === "input"), false);
});

test("model-switch effect resets inherited image count to the new model default and maximum", () => {
  const source = sourceFile("ModelWorkspace.tsx");
  const block = findNode(source, (node) => ts.isIfStatement(node) && node.expression.getText(source) === "isImage" && node.thenStatement.getText(source).includes("setImageCount"));
  for (const [defaults, maximum, expected] of [[{}, 1, 1], [{ count: 9 }, 9, 9], [{ count: 9 }, 1, 1], [{ n: 3 }, 9, 3]]) {
    let count = 9;
    execute(block.getText(source), { isImage: true, defaults, model: { runtime_rule: {} }, imageCountMax: maximum, imageAllowsAutoRatio: false, setImageCount: (value) => { count = value; }, setImageSize: () => {}, setImageRatio: () => {}, defaultImageSizeForConfig: () => "1K", normalizeRatio: (value) => value });
    assert.equal(count, expected);
  }
});

test("submit guards distinguish required first-frame video, optional first-frame and reference images", async () => {
  const source = sourceFile("ModelWorkspace.tsx");
  const handler = findNode(source, (node) => ts.isVariableDeclaration(node) && node.name.getText(source) === "handleMediaTask");
  const statements = handler.initializer.body.statements;
  const stop = statements.findIndex((node) => ts.isVariableStatement(node) && node.declarationList.declarations[0].name.getText(source) === "requestVersion");
  assert.ok(stop > 0);
  const prefix = statements.slice(0, stop).map((node) => node.getText(source)).join("\n");
  for (const [overrides, expected] of [
    [{ videoConfig: { upload_profile: "first_frame", min_reference_images: 1 }, workbenchInputSchema: { required: ["first_frame"] }, videoMedia: { first_frame: { url: "test-image" }, reference_images: [] } }, true],
    [{ videoConfig: { upload_profile: "first_frame", min_reference_images: 1 }, workbenchInputSchema: { required: ["first_frame"] } }, false],
    [{ videoConfig: { upload_profile: "first_frame", min_reference_images: 0 } }, true],
    [{ videoConfig: { upload_profile: "multi_ref", min_reference_images: 1 } }, false],
    [{ videoConfig: { upload_profile: "multi_ref", min_reference_images: 1 }, videoMedia: { reference_images: [{ url: "test-image" }] } }, true],
    [{ prompt: "", videoConfig: { upload_profile: "multi_ref", min_reference_images: 1, prompt_required: false }, videoMedia: { reference_images: [{ url: "test-image" }] } }, true],
    [{ isVideo: false, isImage: true, imageRuntime: { min_reference_images: 1 }, refImages: [] }, false],
    [{ isVideo: false, isAudio: true, workbenchInputSchema: { required: ["voice"], properties: { voice: { type: "string", title: "音色 ID" } } }, params: { voice: " " } }, false],
    [{ isVideo: false, isAudio: true, workbenchInputSchema: { required: ["voice"], properties: { voice: { type: "string", title: "音色 ID" } } }, params: { voice: "voice-id" } }, true],
  ]) {
    const alerts = [];
    const context = { isVideo: true, isAudio: false, isImage: false, prompt: "test prompt", videoConfig: {}, audioConfig: {}, imageRuntime: {}, workbenchInputSchema: {}, params: {}, schemaProperties: (schema) => schema.properties || {}, videoMedia: { reference_images: [] }, refImages: [], isMiniMaxH3: false, isAliyunMultimodal: false, isAliyunHappyHorse: false, isVeoFramePair: false, isVeoReference: false, isOmniReference: false, t: (key) => key, ts: (key) => key, alert: (message) => alerts.push(message), ...overrides };
    execute(`async function validate() { ${prefix}\nreturn true; }`, context);
    assert.equal(await context.validate(), expected ? true : undefined);
    assert.equal(alerts.length, expected ? 0 : 1);
  }
});

test("custom audio/video scalar fields use the existing accessible themed SchemaForm", () => {
  for (const path of ["audio/AudioOptionToolbar.tsx", "video/VideoOptionToolbar.tsx"]) {
    const source = ts.createSourceFile(path, readFileSync(new URL(path, import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    const render = source.statements.find((node) => ts.isFunctionDeclaration(node) && node.name?.text === "renderFieldControl");
    const SchemaForm = () => null;
    const context = { SchemaForm, React: { createElement: (type, props) => ({ type, props }) } };
    vm.runInNewContext(ts.transpileModule(render.getText(source), { compilerOptions: { target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.React } }).outputText, context);
    for (const prop of [{ type: "number", title: "语速", minimum: 0.25 }, { type: "string", title: "音色 ID" }]) {
      const changes = [];
      const element = context.renderFieldControl("custom", prop, undefined, (key, value) => changes.push([key, value]));
      assert.equal(element.type, SchemaForm);
      assert.equal(element.props.schema.properties.custom, prop);
      element.props.onChange({ custom: prop.type === "number" ? 1.2 : "voice-id" });
      assert.deepEqual(changes, [["custom", prop.type === "number" ? 1.2 : "voice-id"]]);
    }
  }
});
