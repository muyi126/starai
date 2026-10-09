import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
import { videoMentionQuery, videoReferenceToken } from "../../../../../../packages/shared-types/src/videoModel.ts";

const source = ts.createSourceFile("VideoPromptTextarea.tsx", readFileSync(new URL("VideoPromptTextarea.tsx", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
function find(predicate) {
  let result;
  const visit = node => { if (!result && predicate(node)) result = node; if (!result) ts.forEachChild(node, visit); };
  visit(source);
  assert.ok(result);
  return result;
}
function execute(code, context) {
  vm.runInNewContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
}

test("selection events do not reset the keyboard-highlighted choice when caret/query have not changed", () => {
  const declaration = find(node => ts.isVariableDeclaration(node) && node.name.getText(source) === "updateQuery");
  let resets = 0;
  const context = {
    input: { current: { value: "@", selectionStart: 1, selectionEnd: 1, getBoundingClientRect: () => ({ left: 10, top: 300, bottom: 400 }) } },
    query: { start: 0, end: 1, query: "" }, videoMentionQuery, lastSelection: { current: {} }, setTab() {},
    setSelected: () => resets++, setQuery() {}, setPosition() {}, window: { innerWidth: 1200 },
  };
  execute(`var ${declaration.getText(source)};`, context);
  context.updateQuery();
  assert.equal(resets, 0);
  context.input.current.value = "@图";
  context.input.current.selectionStart = context.input.current.selectionEnd = 2;
  context.updateQuery();
  assert.equal(resets, 1);
});

test("unchanged browser selection events do not reopen the picker after Escape", () => {
  const attribute = find(node => ts.isJsxAttribute(node) && node.name.getText(source) === "onSelect");
  let opened = 0;
  const context = { props: {}, lastSelection: { current: { value: "@", start: 1, end: 1 } }, updateQuery() { opened++; } };
  execute(`var handler = ${attribute.initializer.expression.getText(source)};`, context);
  context.handler({ currentTarget: { value: "@", selectionStart: 1, selectionEnd: 1 } });
  assert.equal(opened, 0);
  context.handler({ currentTarget: { value: "@图片", selectionStart: 3, selectionEnd: 3 } });
  assert.equal(opened, 1);
});

test("Enter selects a reference, Escape closes the picker, and IME does not select a reference", () => {
  const attribute = find(node => ts.isJsxAttribute(node) && node.name.getText(source) === "onKeyDown");
  let inserted = 0, delegated = 0, closed = 0;
  const context = { query: {}, choices: [{}], selected: 0, insert() { inserted++; }, setSelected() {}, setQuery() { closed++; }, props: { onKeyDown() { delegated++; } } };
  execute(`var handler = ${attribute.initializer.expression.getText(source)};`, context);
  const event = (key, composing = false) => ({ key, nativeEvent: { isComposing: composing }, preventDefault() { this.prevented = true; } });
  const enter = event("Enter"); context.handler(enter);
  assert.equal(enter.prevented, true); assert.equal(inserted, 1); assert.equal(delegated, 0);
  context.handler(event("Escape")); assert.equal(closed, 1);
  context.handler(event("Enter", true)); assert.equal(inserted, 1); assert.equal(delegated, 1);
  context.choices = []; context.handler(event("Enter")); assert.equal(delegated, 1);
});

test("choosing a reference replaces only the query at the cursor and restores the caret", () => {
  const declaration = find(node => ts.isVariableDeclaration(node) && node.name.getText(source) === "insert");
  let result, selection;
  const context = {
    input: { current: { selectionStart: 6, selectionEnd: 6, focus() {}, setSelectionRange(start, end) { selection = [start, end]; } } },
    query: { start: 3, end: 6 }, props: { value: "开头 @图片 后文" }, locale: "zh-CN", videoReferenceToken,
    onValueChange(value) { result = value; }, setQuery() {}, requestAnimationFrame(callback) { callback(); },
  };
  execute(`var ${declaration.getText(source)};`, context);
  context.insert({ kind: "image", index: 2 });
  assert.equal(result, "开头 @图片2  后文"); assert.deepEqual(selection, [8, 8]);
});
