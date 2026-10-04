import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

const source = ts.createSourceFile("login.tsx", readFileSync(new URL("./LoginModal.tsx", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);

test("new user login and password completion navigate despite blocked optional storage", async () => {
  for (const name of ["verifyEmail", "submitPassword"]) {
    let callback;
    function visit(node) {
      if (ts.isVariableDeclaration(node) && node.name.getText(source) === name) callback = node.initializer.getText(source);
      ts.forEachChild(node, visit);
    }
    visit(source);
    assert.ok(callback, `missing login handler ${name}`);
    const events = [], errors = [], users = [];
    const user = { id: 1, email: "fixture@example.test" };
    const context = {
      email: user.email, emailCode: "fixture-code", referralCode: "", password: "fixture-password", confirmPwd: "fixture-password", isNewUser: true, agreed: true,
      t: key => key, setLoading: () => {}, setError: value => { if (value) errors.push(value); },
      api: async () => ({ token: "fixture-cookie-login", user, is_new_user: true, needs_set_password: false }),
      setAuth: (_, value) => users.push(value), onClose: () => events.push("closed"),
      SKIP_FORCED_ANNOUNCEMENT_ONCE_KEY: "optional-announcement",
      window: { localStorage: { setItem: () => { throw new Error("storage disabled"); } }, location: { assign: url => events.push(url) } },
    };
    vm.runInNewContext(ts.transpileModule(`globalThis.submit = ${callback}`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
    await context.submit({ preventDefault() {} });
    assert.deepEqual(events, ["closed", "/app"], `${name} did not complete navigation`);
    assert.deepEqual(errors, [], `${name} misreported a successful login`);
    if (name === "verifyEmail") assert.deepEqual(users, [user]);
  }
});
