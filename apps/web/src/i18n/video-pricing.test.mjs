import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import ts from "typescript";
import vm from "node:vm";
import { MULTIMEDIA_TEMPLATES } from "../../../admin/src/lib/multimedia-templates.ts";
import { translateBuiltinSource, sourceTranslationKey } from "./translation.ts";
import zh from "./locales/zh-CN.ts";
import en from "./locales/en-US.ts";
import ja from "./locales/ja-JP.ts";
import ko from "./locales/ko-KR.ts";
import vi from "./locales/vi-VN.ts";
import enSource from "./source-locales/en-US.ts";
import jaSource from "./source-locales/ja-JP.ts";
import koSource from "./source-locales/ko-KR.ts";
import viSource from "./source-locales/vi-VN.ts";
import videoPricing from "./source-locales/supplements/en-US/videoPricing.ts";

const dictionaries = { "zh-CN": zh, "en-US": en, "ja-JP": ja, "ko-KR": ko, "vi-VN": vi };
const sources = { "en-US": enSource, "ja-JP": jaSource, "ko-KR": koSource, "vi-VN": viSource };
function checkTranslation(label) {
  if (!/[\u3400-\u9fff]/.test(label)) return;
  for (const locale of Object.keys(sources)) {
    const translated = translateBuiltinSource(label.trim(), locale, dictionaries, sources);
    assert.ok(translated, `${locale} missing video/pricing translation: ${label}`);
  }
}

test("all new gateway material labels and descriptions have built-in translations", () => {
  for (const template of MULTIMEDIA_TEMPLATES.filter(t => t.key.startsWith("zex_"))) {
    for (const field of Object.values(template.schema.properties || {})) {
      for (const label of Object.values(field.enumLabels || {})) checkTranslation(label);
      if (field.description) checkTranslation(field.description);
    }
  }
});

test("admin translation catalog includes supplemented canonical keys and config-driven video copy", () => {
  const catalog = JSON.parse(fs.readFileSync(new URL("../../../../services/api/internal/service/ui_translation_catalog.json", import.meta.url), "utf8"));
  for (const [key, value] of Object.entries(zh)) assert.equal(catalog[key], value, `missing canonical catalog entry: ${key}`);
  for (const source of Object.keys(videoPricing)) assert.equal(catalog[sourceTranslationKey(source.trim())], source.trim(), `missing video source catalog entry: ${source}`);
});

test("pricing modal source copy is translated and has no hardcoded Chinese JSX", () => {
  const source = ts.createSourceFile("PricingModal.tsx", fs.readFileSync(new URL("../components/workbench/PricingModal.tsx", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  function strings(node) { if (ts.isStringLiteral(node)) checkTranslation(node.text); ts.forEachChild(node, strings); }
  function visit(node) {
    if (ts.isCallExpression(node) && node.expression.getText(source) === "ts" && node.arguments[0]) strings(node.arguments[0]);
    if (ts.isJsxText(node)) assert.doesNotMatch(node.text, /[\u3400-\u9fff]/, `untranslated pricing JSX: ${node.text.trim()}`);
    ts.forEachChild(node, visit);
  }
  visit(source);
});

test("pricing requests use the selected locale and ignore old results after switching languages or closing", async () => {
  const source = ts.createSourceFile("PricingModal.tsx", fs.readFileSync(new URL("../components/workbench/PricingModal.tsx", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  let effect;
  function visit(node) {
    if (ts.isCallExpression(node) && node.expression.getText(source) === "useEffect" && node.arguments[0]?.getText(source).includes("apiForLocaleCached")) effect = node.arguments[0].getText(source);
    ts.forEachChild(node, visit);
  }
  visit(source);
  assert.ok(effect);
  let models, code = "minimax-h3", loading;
  const requests = [];
  const context = {
    open: true, currentModelCode: "minimax-h3", locale: "en-US", ts: value => value,
    setErr() {}, setLoading: value => { loading = value; }, setModels: value => { models = value; },
    setActiveCode: fn => { code = fn(code); },
    apiForLocaleCached: (path, locale) => new Promise((resolve, reject) => requests.push({ path, locale, resolve, reject })),
  };
  const start = () => { vm.runInNewContext(ts.transpileModule(`var cleanup = (${effect})();`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context); return context.cleanup; };
  const tick = () => new Promise(resolve => setImmediate(resolve));
  let cleanup = start(); cleanup(); context.locale = "ja-JP"; cleanup = start();
  assert.equal(requests[1].locale, "ja-JP");
  requests[1].resolve([{ code: "minimax-h3", display_name: "現在のモデル" }]); await tick();
  requests[0].resolve([{ code: "old", display_name: "Old model" }]); await tick();
  assert.equal(models[0].display_name, "現在のモデル");
  assert.equal(code, "minimax-h3");
  assert.equal(loading, false);
  cleanup(); context.locale = "ko-KR"; cleanup = start(); cleanup();
  requests[2].reject(new Error("offline")); await tick();
  assert.equal(models.length, 0, "closed or switched panel must ignore a late failed request");
});
