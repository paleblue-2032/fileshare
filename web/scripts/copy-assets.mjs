import {cp, mkdir, readdir, rm} from 'node:fs/promises';
import {resolve} from 'node:path';

const root = resolve(import.meta.dirname, '..');
const dist = resolve(root, 'dist');
const out = resolve(root, '..', 'internal', 'web', 'assets');

await rm(out, {recursive: true, force: true});
await mkdir(out, {recursive: true});

for (const name of ['index.html', 'login.html', 'favicon.svg']) {
  await cp(resolve(dist, name), resolve(out, name));
}

for (const name of await readdir(resolve(dist, 'assets'))) {
  await cp(resolve(dist, 'assets', name), resolve(out, name), {recursive: true});
}

console.log(`copied web assets to ${out}`);
