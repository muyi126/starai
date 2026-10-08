import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';

test('server config uses internal API when provided and preserves cached public fallback', async () => {
  const source = readFileSync(new URL('./public-config.ts', import.meta.url), 'utf8');
  const js = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
  for (const [env, expected] of [
    [{ INTERNAL_API_URL: 'http://api:8080', NEXT_PUBLIC_API_URL: 'https://public.example.test' }, 'http://api:8080'],
    [{ NEXT_PUBLIC_API_URL: 'https://public.example.test' }, 'https://public.example.test'],
    [{}, 'http://localhost:8080'],
  ]) {
    const exports = {};
    let request;
    vm.runInNewContext(js, { exports, process: { env }, AbortSignal, fetch: async (url, options) => {
      request = { url, options };
      return { ok: true, json: async () => ({ data: { site_name: 'Fixture' } }) };
    } });
    assert.equal(exports.API_URL, expected);
    assert.equal((await exports.getPublicSystemConfig()).site_name, 'Fixture');
    assert.equal(request.url, `${expected}/api/system-configs/public`);
    assert.equal(request.options.next.revalidate, 60);
    assert.ok(request.options.signal);
  }
});
