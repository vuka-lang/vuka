// Organize imports on save: the defaults the extension contributes for both
// languages, its setting, and the middleware that honours the setting and
// never holds a save past its wait.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const { organizeKind, asksOrganize, organizeMiddleware } = require('../organize');
const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'));

const defaults = pkg.contributes.configurationDefaults;
for (const lang of ['[vuka]', '[vuka-html]']) {
  assert.deepEqual(defaults[lang]['editor.codeActionsOnSave'], { 'source.organizeImports': 'explicit' }, lang);
  assert.equal(defaults[lang]['editor.formatOnSave'], true, lang);
}
const setting = pkg.contributes.configuration.properties['vuka.organizeImportsOnSave'];
assert.equal(setting.type, 'boolean');
assert.equal(setting.default, true);

assert.ok(asksOrganize({ only: { value: organizeKind } }));
assert.ok(asksOrganize({ only: organizeKind }));
assert.ok(!asksOrganize({ only: { value: 'quickfix' } }));
assert.ok(!asksOrganize({}));

const action = { title: 'Organize imports', kind: organizeKind };
const onSave = { only: { value: organizeKind }, triggerKind: 2 };
const invoked = { only: { value: organizeKind }, triggerKind: 1 };
const answers = (value, ms = 0) => () => new Promise((resolve) => setTimeout(() => resolve(value), ms));

// On: the server's answer.
let on = true;
const mw = organizeMiddleware(() => on, 100);
assert.deepEqual(await mw(null, null, onSave, null, answers([action])), [action]);
// Off: a save gets nothing and doesn't ask; the command still organizes.
on = false;
let asked = false;
assert.deepEqual(await mw(null, null, onSave, null, () => ((asked = true), [action])), []);
assert.equal(asked, false);
assert.deepEqual(await mw(null, null, invoked, null, answers([action])), [action]);
// Late: the save goes on without it.
on = true;
const started = Date.now();
assert.deepEqual(await mw(null, null, onSave, null, answers([action], 1000)), []);
assert.ok(Date.now() - started < 900, 'waited for a late answer');
// Other requests pass through untouched.
const quickfix = { only: { value: 'quickfix' }, triggerKind: 2 };
assert.deepEqual(await mw(null, null, quickfix, null, () => 'next'), 'next');
console.log('organize tests passed');
