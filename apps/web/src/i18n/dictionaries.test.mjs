import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";

// Execute the actual loader functions with controlled chunks, not real network IO.
const source = ts.createSourceFile("dictionaries.ts", readFileSync(new URL("./dictionaries.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const code = source.statements.filter((node) => ts.isFunctionDeclaration(node) || ts.isVariableStatement(node) && node.declarationList.declarations[0].name.getText(source) === "localeLoads").map((node) => node.getText(source).replace(/^export /, "")).join("\n");

test("parallel requests share dictionary/source chunks and resolved requests perform no extra loads", async () => {
  let dictionaryLoads = 0, sourceLoads = 0, resolveDictionary;
  const dictionary = new Promise((resolve) => { resolveDictionary = resolve; });
  const context = { zhCN: {}, dictionaries: {}, sourceTranslations: {}, dictionaryLoaders: { "en-US": () => { dictionaryLoads++; return dictionary; } }, sourceTranslationLoaders: { "en-US": async () => { sourceLoads++; return { default: { 原文: "English" } }; } } };
  vm.runInNewContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  const first = context.loadLocaleDictionaries("en-US"), second = context.loadLocaleDictionaries("en-US");
  assert.equal(first, second);
  assert.equal(dictionaryLoads, 1);
  assert.equal(sourceLoads, 1);
  resolveDictionary({ default: { key: "English" } });
  await Promise.all([first, second]);
  await context.loadLocaleDictionaries("en-US");
  assert.equal(dictionaryLoads, 1);
  assert.equal(sourceLoads, 1);
});

test("a failed source chunk can retry without discarding an already loaded key dictionary", async () => {
  let sourceLoads = 0, dictionaryLoads = 0;
  const context = { zhCN: {}, dictionaries: {}, sourceTranslations: {}, dictionaryLoaders: { "en-US": async () => { dictionaryLoads++; return { default: { key: "English" } }; } }, sourceTranslationLoaders: { "en-US": async () => { if (++sourceLoads === 1) throw new Error("chunk unavailable"); return { default: { 原文: "English" } }; } } };
  vm.runInNewContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  await assert.rejects(context.loadLocaleDictionaries("en-US"), /chunk unavailable/);
  await context.loadLocaleDictionaries("en-US");
  assert.equal(dictionaryLoads, 1);
  assert.equal(sourceLoads, 2);
  assert.equal(context.sourceTranslations["en-US"].原文, "English");
});
