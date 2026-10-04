import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';

test('landing gallery waits until approached, fetches once and ignores unmounted results', async () => {
  const file = ts.createSourceFile('LandingPageClient.tsx', readFileSync(new URL('./LandingPageClient.tsx', import.meta.url), 'utf8'), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  let effect;
  function visit(node) {
    if (ts.isCallExpression(node) && node.expression.getText(file) === 'useEffect' && node.arguments[0]?.getText(file).includes('observer.observe(section)')) effect = node.arguments[0].getText(file);
    ts.forEachChild(node, visit);
  }
  visit(file);
  assert.ok(effect, 'real gallery effect is present');
  for (const supported of [true, false]) {
    let observe, cleanup, fetches = 0, updates = 0, disconnects = 0, resolve;
    const manifest = new Promise(r => { resolve = r; });
    const context = {
      gallerySectionRef: { current: {} }, LANDING_GALLERY_LIMIT: 12,
      loadReferenceGalleryManifest: () => { fetches++; return manifest; },
      randomReferenceCases: items => items, setGallery: () => { updates++; }, setGalleryLoading: () => { updates++; },
      IntersectionObserver: supported ? class {
        constructor(callback, options) { observe = callback; assert.equal(options.rootMargin, '300px'); }
        observe() {} disconnect() { disconnects++; }
      } : undefined,
    };
    const js = ts.transpileModule(`const cleanup = (${effect})(); globalThis.cleanup = cleanup;`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
    vm.runInNewContext(js, context);
    cleanup = context.cleanup;
    assert.equal(fetches, supported ? 0 : 1);
    if (supported) {
      observe([{ isIntersecting: false }]); assert.equal(fetches, 0);
      observe([{ isIntersecting: true }]); assert.equal(fetches, 1); assert.equal(disconnects, 1);
    }
    cleanup(); resolve({ cases: [] });
    await manifest; await new Promise(r => setImmediate(r));
    assert.equal(updates, 0, 'late results do not update an unmounted page');
  }
});
