import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const { markupStart, markupBefore, tagToClose, closingFor } = require('../shape');
const read = (p) => JSON.parse(readFileSync(new URL(p, import.meta.url), 'utf8'));
const pkg = read('../package.json');
const language = pkg.contributes.languages.find((l) => l.id === 'vuka-html');

for (const file of ['[id].vuka.html', '[path...].vuka.html', '_layout.vuka.html', 'pets/[id]/index.vuka.html', '(admin)/_layout.vuka.html', 'admin/(console)/users/[id].vuka.html', '_parts/card.vuka.html']) {
  assert.ok(language.extensions.some((suffix) => file.endsWith(suffix)), file);
}
assert.ok(pkg.activationEvents.includes('onLanguage:vuka-html'));
const grammar = pkg.contributes.grammars.find((g) => g.language === 'vuka-html');
assert.equal(grammar.embeddedLanguages['meta.embedded.block.go'], 'go');
assert.equal(grammar.tokenTypes['meta.embedded.block.go'], 'other');

const fixture = readFileSync(new URL('./fixtures-html/shape.vuka.html', import.meta.url), 'utf8');
assert.equal(markupStart(fixture), fixture.indexOf('<main class'));
const header = fixture.slice(0, fixture.indexOf('<main class'));
for (let end = 0; end <= header.length; end++) {
  assert.equal(markupStart(header.slice(0, end)), -1, `header prefix at ${end}`);
}
for (const prefix of [
  'package p\nimport (\n C "./C.vue"\n)\ntype P struct {}\n',
  'package p\ntype P struct {}\nfunc (p *P) f() {\n <-ch\n if a < b { f() }\n}\n',
  'package p\ntype P struct {}\nvar raw = `\n<div>\n`\n',
  'package p\ntype P struct {}\n/*\n<div>\n*/\n',
  'package p\ntype P struct {}\nvar quote = "\\\"<div>"\n',
]) {
  assert.equal(markupStart(prefix + '  <main>'), prefix.length + 2);
}
assert.equal(markupStart('<-ch\n'), -1);
assert.equal(markupStart('<main>'), 0);
assert.equal(markupBefore({ languageId: 'vuka' }, 'return <div>'), null);
assert.equal(markupBefore({ languageId: 'go' }, '<div>'), null);
assert.equal(markupBefore({ languageId: 'vuka-html' }, header), null);
assert.equal(tagToClose(markupBefore({ languageId: 'vuka-html' }, header + '<main>')), 'main');
assert.equal(closingFor(markupBefore({ languageId: 'vuka-html' }, header + '<main><components.Item></'), ''), 'components.Item>');

for (const code of [
  '<main>{"<fake>"', '<main>{`<fake>`', '<main><!-- <fake>',
  '<script lang="ts">const x = "<fake>"', '<style>.x::after { content: "<fake>"',
  '<main>\n  if label == "<fake>"',
]) {
  assert.equal(tagToClose(code), null, code);
  assert.equal(closingFor(code + '</', ''), null, code);
}
assert.equal(closingFor('<main>{"<fake>"}</', ''), 'main>');
assert.equal(closingFor('<main><!-- <fake> --></', ''), 'main>');
assert.equal(closingFor('<main>\n if open {\n  <b></b>\n }\n</', '>'), 'main');
assert.equal(tagToClose('<button onClick={open = false; Save()}>'), 'button');
assert.equal(tagToClose('<Week client:visible />'), null);
assert.equal(tagToClose('<input bind:value={Q}>'), null);

// Expand defaults using the syntax present in these snippets. Fail unknown
// placeholders rather than silently accepting a malformed expansion.
function expand(body) {
  const values = new Map();
  let text = body.join('\n').replace(/\$\{(\d+)(?::([^}]*?)|\|([^}]*?)\|)\}/g, (_, id, value, choices) => {
    const chosen = value ?? choices.split(',')[0];
    values.set(id, chosen);
    return chosen;
  });
  text = text.replace(/\$(\d+)/g, (_, id) => {
    if (id === '0') return '';
    assert.ok(values.has(id), `undefined tabstop $${id}`);
    return values.get(id);
  });
  assert.ok(!text.includes('${'), 'unexpanded placeholder');
  return text;
}
const htmlSnippets = read('../snippets/vuka-html.json');
const logicSnippets = read('../snippets/vuka.json');
for (const language of ['vuka', 'vuka-html']) {
  assert.ok(pkg.contributes.snippets.some((s) => s.language === language));
}
for (const name of ['Live page shape', 'Static component shape', 'Folder layout shape', 'Island-only page shape']) {
  const code = expand(htmlSnippets[name].body);
  assert.match(code, /^package /);
  assert.equal((code.match(/\btype \w+ struct/g) || []).length, 1, name);
  assert.ok(markupStart(code) > 0, name);
  assert.doesNotMatch(code, /\bRender\(|ui\.(?:UseState|LiveSchema|IslandPage)|@web\.(?:Get|Live|Service)/);
}
const hook = expand(htmlSnippets['Mount hook'].body);
assert.match(hook, /^@ui.Mount\nfunc \(p \*Index\) mount/);
const api = expand(logicSnippets['API endpoint'].body);
assert.match(api, /func GET\(ctx context.Context\)/);
assert.match(api, /func POST\(ctx context.Context, input Input\)/);
assert.equal(markupStart(api), -1);
for (const snippet of Object.values(logicSnippets)) {
  const code = expand(snippet.body);
  assert.doesNotMatch(code, /return </);
  assert.doesNotMatch(code, /ui\.(?:UseState|LiveSchema)|@web\.(?:Get|Live|Service)/);
}
console.log('ok shape boundaries, route associations and snippets');
