import fs from 'node:fs';
import path from 'node:path';
import { gzipSync } from 'node:zlib';

// Sum unique production route-entry scripts, including every parent layout.
// Async interaction/workspace chunks are measured separately in browser checks.
const args = process.argv.slice(2);
const option = (key, fallback) => args.includes(key) ? args[args.indexOf(key) + 1] : fallback;
const dist = option('--dist', '.next');
const result = {};
for (const app of ['web', 'admin']) {
  const directory = path.resolve('apps', app, dist);
  if (!fs.existsSync(path.join(directory, 'BUILD_ID'))) throw Error(`${app}: build production output first`);
  const manifest = JSON.parse(fs.readFileSync(path.join(directory, 'app-build-manifest.json'), 'utf8'));
  const pages = {};
  for (const [page, files] of Object.entries(manifest.pages)) {
    if (!page.endsWith('/page')) continue;
    const route = page.slice(0, -5) || '/';
    const parts = route.split('/').filter(Boolean);
    const scripts = new Set(files.filter(file => file.endsWith('.js')));
    for (let i = 0; i <= parts.length; i++) {
      const layout = '/' + parts.slice(0, i).join('/') + (i ? '/' : '') + 'layout';
      for (const file of manifest.pages[layout] || []) if (file.endsWith('.js')) scripts.add(file);
    }
    let rawBytes = 0, gzipBytes = 0;
    for (const file of scripts) {
      const bytes = fs.readFileSync(path.join(directory, file));
      rawBytes += bytes.length;
      gzipBytes += gzipSync(bytes).length;
    }
    pages[route] = { scripts: scripts.size, rawBytes, gzipBytes };
  }
  result[app] = pages;
}
const output = JSON.stringify(result, null, 2);
const destination = option('--output', '');
if (destination) fs.writeFileSync(destination, output + '\n', 'utf8');
console.log(output);
