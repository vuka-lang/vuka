import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const { extractCommand, nameProblem, extractArguments } = require('../refactor');
const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'));

// The command is declared, and kept out of the palette: it needs a selection's code action.
assert.ok(pkg.contributes.commands.some((c) => c.command === extractCommand));
assert.ok(pkg.contributes.menus.commandPalette.some((m) => m.command === extractCommand && m.when === 'false'));

for (const ok of ['Card', 'orderRow', 'Ünits', '_Part2']) assert.equal(nameProblem(ok), undefined, ok);
for (const bad of ['', '2Card', 'order-row', 'a b', undefined]) assert.ok(nameProblem(bad), String(bad));

const range = { start: { line: 3, character: 2 }, end: { line: 5, character: 0 } };
assert.deepEqual(extractArguments({ uri: 'file:///w/a.vuka.html', range }, 'Card'), [{ uri: 'file:///w/a.vuka.html', range, name: 'Card' }]);
console.log('refactor tests passed');
