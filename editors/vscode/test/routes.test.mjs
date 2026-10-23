import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const { tokenTypes, formatRoute, moduleRoot } = require('../routes');
const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'));

// Every token type the server adds is declared, with a scope to fall back to.
const declared = pkg.contributes.semanticTokenTypes.map((t) => t.id);
const scopes = pkg.contributes.semanticTokenScopes.find((s) => s.language === 'vuka-html').scopes;
for (const t of tokenTypes) {
  assert.ok(declared.includes(t), t);
  assert.ok(scopes[t] && scopes[t].length, t);
}
assert.equal(new Set(Object.values(scopes).map((s) => s[s.length - 1])).size, tokenTypes.length, 'each type falls back to its own scope');
assert.ok(pkg.contributes.commands.some((c) => c.command === 'vuka.showRoute'));

assert.equal(
  formatRoute({ method: 'GET', path: '/orgs/{org}', page: 'org.Index', kind: 'live', file: 'pages/orgs/[org]/index.vuka.html', line: 10,
    middleware: ['pages.Middleware'], guards: ['org.Guard'], layouts: ['pages.Layout', 'org.Layout'] }),
  'GET /orgs/{org}\n  page        org.Index (live)\n  declared    pages/orgs/[org]/index.vuka.html:10\n' +
    '  middleware  pages.Middleware\n  guards      org.Guard\n  layouts     pages.Layout, org.Layout',
);
const files = new Set(['/w/app/go.mod']);
assert.equal(moduleRoot('/w/app/pages/orgs', (p) => files.has(p)), '/w/app');
assert.equal(moduleRoot('/w/other', (p) => files.has(p)), undefined);
console.log('routes tests passed');
