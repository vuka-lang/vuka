// Configuration the extension contributes for other extensions — Tailwind
// CSS IntelliSense, Emmet, sticky scroll — checked against fixtures: the
// class regexes run the way Tailwind runs them, and the header's scopes
// stay Go.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createHighlighter } from 'shiki';

const read = (p) => JSON.parse(readFileSync(new URL(p, import.meta.url), 'utf8'));
const pkg = read('../package.json');
const defaults = pkg.contributes.configurationDefaults;

assert.deepEqual(defaults['tailwindCSS.includeLanguages'], { 'vuka-html': 'html', vuka: 'html' });
assert.deepEqual(defaults['emmet.includeLanguages'], { 'vuka-html': 'html' });
assert.ok(!('vuka' in defaults['emmet.includeLanguages']), 'Emmet stays off in .vuka logic files');
const filters = defaults['tailwindCSS.experimental.classRegex'];
assert.ok(Array.isArray(filters) && filters.length > 0);

// classLists is Tailwind IntelliSense's reading of experimental.classRegex: a
// string is a regex whose group 1 is a class list; a pair's first regex finds
// a container (group 1), its second the class lists in it.
function classLists(text) {
  const out = [];
  for (const f of filters) {
    const [container, inner] = Array.isArray(f) ? f : [f];
    for (const m of text.matchAll(new RegExp(container, 'gd'))) {
      assert.notEqual(m[1], undefined, `/${container}/ needs one capture group`);
      const base = m.indices[1][0];
      if (!inner) {
        out.push({ list: m[1], at: base });
        continue;
      }
      for (const n of m[1].matchAll(new RegExp(inner, 'gd'))) {
        assert.notEqual(n[1], undefined, `/${inner}/ needs one capture group`);
        out.push({ list: n[1], at: base + n.indices[1][0] });
      }
    }
  }
  return out;
}

const fixture = readFileSync(new URL('./fixtures-tailwind/classes.vuka.html', import.meta.url), 'utf8');
const found = classLists(fixture);
const lists = found.map((f) => f.list.trim()).filter(Boolean).sort();
assert.deepEqual(lists, [
  'bg-blue-600 text-white',
  'border-b px-4',
  'flex gap-2',
  'mt-2',
  'mx-auto max-w-3xl',
  'px-5',
  'text-2xl font-bold',
  'text-gray-700',
  'text-sm',
].sort());
const at = found.map((f) => f.at);
assert.equal(new Set(at).size, at.length, 'a class list found twice');
for (const f of found) {
  assert.equal(fixture.slice(f.at, f.at + f.list.length), f.list, 'ranges point at the text');
}
// The regexes never run away on a long line.
const long = '<p class={' + '"a" + '.repeat(2000) + '"b"}>' + 'x'.repeat(20000);
const t0 = Date.now();
classLists(long);
assert.ok(Date.now() - t0 < 500, 'class regexes are slow on a long line');

// Emmet reads the document as HTML; the grammar keeps the Go header Go, so
// brackets, comments and snippets there are Go's.
const html = read('../syntaxes/vuka-html.tmLanguage.json');
const highlighter = await createHighlighter({
  themes: ['github-dark'],
  langs: ['go', 'typescript', 'css', 'html', { ...html, name: 'vuka-html', embeddedLangs: ['go', 'typescript', 'css', 'html'] }],
});
function scopesAt(code, needle) {
  const at = code.indexOf(needle);
  assert.ok(at >= 0, needle);
  let off = 0;
  for (const line of highlighter.codeToTokensBase(code, { lang: 'vuka-html', theme: 'github-dark', includeExplanation: 'scopeName' })) {
    for (const t of line) {
      for (const e of t.explanation) {
        if (at >= off && at < off + e.content.length) return e.scopes.map((s) => s.scopeName);
        off += e.content.length;
      }
    }
    off++;
  }
  return [];
}
for (const needle of ['package pages', 'type Index', 'return "bg-blue', 'func (p *Index) rowClasses']) {
  const scopes = scopesAt(fixture, needle);
  assert.ok(scopes.includes('meta.embedded.block.go'), `${needle}: ${scopes}`);
  assert.ok(!scopes.some((s) => s.startsWith('text.html.basic') || s.startsWith('meta.tag')), `${needle}: ${scopes}`);
}
for (const needle of ['<main', '<h1 class']) {
  const scopes = scopesAt(fixture, needle);
  assert.ok(!scopes.includes('meta.embedded.block.go'), `${needle}: ${scopes}`);
}
console.log('editors: ok');
