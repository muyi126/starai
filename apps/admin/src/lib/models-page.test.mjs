import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";

const source = ts.createSourceFile("models.tsx", readFileSync(new URL("../app/admin/models/page.tsx", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
const compile = (text) => ts.transpileModule(text, { compilerOptions: { target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.React } }).outputText;

test("lazy form editors have local loading boundaries to keep the portal mount attached", () => {
  for (const name of ["UpstreamIncludeEditor", "ModelRoutesEditor"]) {
    const statement = source.statements.find((node) => ts.isVariableStatement(node) && node.declarationList.declarations.some((declaration) => declaration.name.getText(source) === name));
    const options = vm.runInNewContext(compile(`${statement.getText(source)}; ${name};`), {
      dynamic: (_loader, options) => options,
      React: { createElement: (tag, props) => ({ tag, props }) },
    });
    assert.equal(typeof options?.loading, "function", `${name} must contain its own Suspense boundary`);
    assert.equal(options.loading().props.role, "status");
  }
});

test("status toggles send only the status and report request or refresh failures", async () => {
  const page = source.statements.find((node) => ts.isFunctionDeclaration(node) && node.name?.text === "ModelsPage");
  const statement = page.body.statements.find((node) => ts.isVariableStatement(node) && node.declarationList.declarations.some((declaration) => declaration.name.getText(source) === "toggleEnabled"));
  for (const enabled of [true, false]) {
    for (const failure of ["", "request", "refresh"]) {
      const calls = [];
      let message;
      const toggle = vm.runInNewContext(compile(`${statement.getText(source)}; toggleEnabled;`), {
        Error,
        setErr: (value) => { message = value; },
        adminApi: async (path, options) => {
          calls.push({ path, enabled: JSON.parse(options.body).is_enabled, fields: Object.keys(JSON.parse(options.body)) });
          assert.equal(options.method, "PATCH");
          if (failure === "request") throw new Error("request failed");
        },
        load: async () => {
          if (failure === "refresh") throw new Error("refresh failed");
        },
      });
      await toggle({ id: 7, is_enabled: enabled, new_api_extra_params: { connection: { api_key: "masked" } } });
      assert.equal(calls[0].path, "/models/7/status");
      assert.equal(calls[0].enabled, !enabled);
      assert.deepEqual(calls[0].fields, ["is_enabled"]);
      assert.equal(message, failure ? `${failure} failed` : "");
    }
  }
});
