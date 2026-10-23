// .vuka.html grammar test: tokenizes test/fixtures-html/*.vuka.html and checks
// the scopes of its holes, statements, header, script and style.
import { readFileSync, writeFileSync, readdirSync } from 'node:fs';
import assert from 'node:assert/strict';
import { createHighlighter } from 'shiki';

const read = (p) => JSON.parse(readFileSync(new URL(p, import.meta.url), 'utf8'));
const html = read('../syntaxes/vuka-html.tmLanguage.json');
const vuka = read('../syntaxes/vuka.tmLanguage.json');
const inject = read('../syntaxes/vuka-html-injection.tmLanguage.json');
const highlighter = await createHighlighter({
  themes: ['github-dark'],
  langs: ['go', 'typescript', 'css', 'html',
    { ...vuka, name: 'vuka', embeddedLangs: ['go'] },
    { ...html, name: 'vuka-html', embeddedLangs: ['go', 'typescript', 'css', 'html'] },
    { ...inject, name: 'vuka-html-injection', injectTo: ['source.go'], embeddedLangs: ['vuka-html'] }],
});

let failures = 0;
function scopesAt(code, lang, needle) {
  const at = code.indexOf(needle);
  assert.ok(at >= 0, `missing assertion text: ${needle}`);
  let off = 0;
  for (const line of highlighter.codeToTokensBase(code, { lang, theme: 'github-dark', includeExplanation: 'scopeName' })) {
    for (const t of line) {
      for (const e of t.explanation) {
        if (at >= off && at < off + e.content.length) return e.scopes.map((s) => s.scopeName);
        off += e.content.length;
      }
    }
    off++; // the line break
  }
  return [];
}
function expect(code, lang, needle, scope) {
  const got = scopesAt(code, lang, needle);
  if (!got.some((s) => s.startsWith(scope))) {
    failures++;
    console.error(`FAIL ${JSON.stringify(needle)}: ${got.join(' ')}, want ${scope}`);
  }
}

const code = readFileSync(new URL('./fixtures-html/sales.vuka.html', import.meta.url), 'utf8');
expect(code, 'vuka-html', 'Title}', 'source.go');
expect(code, 'vuka-html', 'range Week', 'keyword');
expect(code, 'vuka-html', 'func row', 'keyword');
expect(code, 'vuka-html', 'Order)', 'entity.name.type.go');
expect(code, 'vuka-html', '<section', 'punctuation.definition.tag');
expect(code, 'vuka-html', 'class:big', 'entity.other.attribute-name');
expect(code, 'vuka-html', 'len(Week)', 'source.go');
expect(code, 'vuka-html', '{{literal', 'constant.character.escape');
expect(code, 'vuka-html', 'component<Sales>', 'source.ts');
expect(code, 'vuka-html', 'color: red', 'source.css');
expect(code, 'vuka-html', 'Clock "', 'entity.name.import');

// Legacy signatures remain tolerant grammar input; snippets advertise only shapes.
const hdr = readFileSync(new URL('./fixtures-html/header.vuka.html', import.meta.url), 'utf8');
expect(hdr, 'vuka-html', 'func navLink', 'keyword');
expect(hdr, 'vuka-html', 'navLink(item', 'entity.name.function');
expect(hdr, 'vuka-html', 'Item, on', 'entity.name.type');
expect(hdr, 'vuka-html', 'ui.Link href', 'entity.name.tag');
expect(hdr, 'vuka-html', 'href={', 'entity.other.attribute-name');
expect(hdr, 'vuka-html', 'item.link()', 'source.go');
expect(hdr, 'vuka-html', 'components.Icon', 'entity.name.tag');
expect(hdr, 'vuka-html', 'glyph=', 'entity.other.attribute-name');
expect(hdr, 'vuka-html', '"shrink-0"', 'string.quoted.double');
expect(hdr, 'vuka-html', 'span class', 'entity.name.tag');
expect(hdr, 'vuka-html', '</ui.Link', 'punctuation.definition.tag');
expect(hdr, 'vuka-html', 'func (c', 'keyword');
expect(hdr, 'vuka-html', 'badge()', 'entity.name.function');
expect(hdr, 'vuka-html', 'b class="badge"', 'entity.name.tag');

// The package clause, grouped imports, and lowercase tags naming templates.
expect(hdr, 'vuka-html', 'package layouts', 'keyword.other.package');
expect(hdr, 'vuka-html', 'layouts\n', 'entity.name.type.package');
expect(hdr, 'vuka-html', '"strings"', 'string.quoted.double.go');
expect(hdr, 'vuka-html', 'c "portal', 'entity.name.import');
expect(hdr, 'vuka-html', 'navLink item', 'support.class.component');
expect(hdr, 'vuka-html', 'my-chart>', 'entity.name.tag.html');
expect(hdr, 'vuka-html', 'nav>{', 'entity.name.tag.html');
expect(code, 'vuka-html', 'package sales', 'keyword.other.package');

// Compiler-output/interop raw strings retain their existing highlighting.
const generated = 'package p\nfunc render() { return ui.HTML(`<p>{title}</p>`) }\n';
expect(generated, 'go', '<p>', 'punctuation.definition.tag');
expect(generated, 'go', 'title}', 'source.go');

// Shape grammar regressions: all boundary and markup cases are in one fixture.
const shape = readFileSync(new URL('./fixtures-html/shape.vuka.html', import.meta.url), 'utf8');
const cases = [
  ['package pages', 'keyword.other.package.go'],
  ['Week "', 'entity.name.import.go'],
  ...['Week.vue', 'Chart.tsx', 'Widget.jsx', 'Panel.svelte', 'Vanilla.ts', 'Script.js']
    .map((name) => [name, 'string.quoted.double.go']),
  ['type Index', 'keyword'],
  ['struct {', 'keyword'],
  ['@ui.Mount', 'meta.attribute.vuka'],
  ['@ui.OnChange', 'meta.attribute.vuka'],
  ['@ui.Loads', 'meta.attribute.vuka'],
  ['@ui.Bind', 'meta.attribute.vuka'],
  ['@ui.URL', 'meta.attribute.vuka'],
  ['@ui.Defer', 'meta.attribute.vuka'],
  ['func (p *Index) mount', 'keyword'],
  ['<-ch', 'source.go'],
  ['< "z"', 'source.go'],
  ['<div>not markup', 'string.quoted.raw'],
  ['<aside>not a boundary', 'comment'],
  ['<section>not a boundary', 'comment'],
  ['<main class', 'punctuation.definition.tag'],
  ['components.Item key', 'support.class.component'],
  ['key={t.ID}', 'entity.other.attribute-name'],
  ['todo={t}', 'entity.other.attribute-name'],
  ['t.ID}', 'source.go'],
  ['{Q}</p>', 'punctuation.section.embedded.begin'],
  ['if editing', 'meta.statement.vuka'],
  ['} else {', 'meta.statement.vuka'],
  ['for _, t', 'meta.statement.vuka'],
  ['class:active', 'entity.other.attribute-name'],
  ['style:color', 'entity.other.attribute-name'],
  ['bind:value', 'entity.other.attribute-name'],
  ...['visible', 'load', 'idle', 'only', 'media'].map((s) => ['client:' + s, 'entity.other.attribute-name']),
  ['editing = !editing', 'source.go'],
  ['tab = "env"', 'source.go'],
  ['open = false', 'source.go'],
  ['Save()}>', 'source.go'],
  ['slot:actions', 'entity.name.tag.html'],
  ['color: red', 'source.css'],
  ['const label', 'source.ts'],
];
for (const [needle, scope] of cases) expect(shape, 'vuka-html', needle, scope);
for (const needle of ['<-ch', '<div>not markup', '<section>not a boundary']) {
  if (scopesAt(shape, 'vuka-html', needle).some((s) => s.startsWith('meta.tag'))) {
    failures++;
    console.error(`FAIL ${needle}: Go header was treated as markup`);
  }
}

// The script form: the Go is the <script lang="go">'s, the markup follows.
const script = readFileSync(new URL('./fixtures-html/script.vuka.html', import.meta.url), 'utf8');
for (const [needle, scope] of [
  ['The index page', 'comment.block.html'],
  ['script lang="go"', 'entity.name.tag.html'],
  ['package pages', 'keyword.other.package.go'],
  ['Week "', 'entity.name.import.go'],
  ['type Index', 'keyword'],
  ['@ui.Mount', 'meta.attribute.vuka'],
  ['<-ch', 'source.go'],
  ['</script> in a string', 'string.quoted.double'],
  ['<div>not markup', 'string.quoted.raw'],
  ['<aside>not a boundary', 'comment'],
  ['<main class', 'punctuation.definition.tag'],
  ['if editing', 'meta.statement.vuka'],
  ['color: red', 'source.css'],
  ['const label', 'source.ts'],
]) expect(script, 'vuka-html', needle, scope);
for (const needle of ['<main class', 'Read only', '<style scoped>', '<script lang="ts"']) {
  assert.ok(!scopesAt(script, 'vuka-html', needle).includes('meta.embedded.block.go'), needle);
}
for (const needle of ['package pages', 'type Index', '<-ch']) {
  assert.ok(scopesAt(script, 'vuka-html', needle).includes('meta.embedded.block.go'), needle);
}

// Every token has a reviewable expected scope, as in the .vuka fixtures.
// Snapshot creation is explicit: a missing snapshot is a test failure.
const dir = new URL('./fixtures-html/', import.meta.url);
const update = process.argv.includes('--update');
for (const file of readdirSync(dir).filter((f) => f.endsWith('.vuka.html')).sort()) {
  const source = readFileSync(new URL(file, dir), 'utf8');
  const lines = highlighter.codeToTokensBase(source, {
    lang: 'vuka-html', theme: 'github-dark', includeExplanation: 'scopeName',
  });
  const snapshot = lines.map((tokens, i) => `${i + 1}:\n` + tokens.flatMap((t) =>
    t.explanation.filter((e) => e.content.trim()).map((e) =>
      `  ${JSON.stringify(e.content)} ${e.scopes.map((s) => s.scopeName).join(' ')}`),
  ).join('\n')).join('\n').trimEnd() + '\n';
  const path = new URL(file + '.snap', dir);
  if (update) writeFileSync(path, snapshot);
  else assert.equal(readFileSync(path, 'utf8'), snapshot, file + ' scopes changed');
  if (['[id].vuka.html', '[path...].vuka.html', '_layout.vuka.html'].includes(file)) {
    expect(source, 'vuka-html', 'package ', 'keyword.other.package.go');
    expect(source, 'vuka-html', source.match(/^<[^\n]+/m)[0], 'punctuation.definition.tag');
  }
}
// The transition is permanent: markup and its text are outside the Go header.
for (const needle of ['<main class', 'Read only', '<style scoped>', '<script lang']) {
  assert.ok(!scopesAt(shape, 'vuka-html', needle).includes('meta.embedded.block.go'), needle);
}
expect(shape, 'vuka-html', 'ui.Mount', 'entity.name.function.decorator');
expect(shape, 'vuka-html', 'ui.Bind', 'entity.name.function.decorator');
expect(shape, 'vuka-html', '500 *', 'constant.numeric');
expect(shape, 'vuka-html', '}\n  for', 'punctuation.definition.block.end.go');

if (failures) process.exit(1);
console.log('ok html grammar');
