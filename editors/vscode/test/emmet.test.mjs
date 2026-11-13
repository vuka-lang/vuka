// Emmet in .vuka.html: abbreviations expand in a text node of the markup,
// and nowhere in the Go header, a Go hole, a tag, a comment, a script or
// style body or a statement line.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const { emmetCompletions, triggerCharacters } = require('../emmet');
const { textNodeStart } = require('../shape');
const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'));

const config = { showExpandedAbbreviation: 'always', showAbbreviationSuggestions: true };
const fixture = readFileSync(new URL('./fixtures-html/shape.vuka.html', import.meta.url), 'utf8');
const script = readFileSync(new URL('./fixtures-html/script.vuka.html', import.meta.url), 'utf8');
const header = fixture.slice(0, fixture.indexOf('<main class'));

// expand types text at the cursor | (or at the end) after prefix and
// returns { label: newText } of what Emmet offers there, or null.
function expand(prefix, typed, opts = config, languageId = 'vuka-html') {
  const at = typed.indexOf('|');
  const text = prefix + typed.replace('|', '');
  const list = emmetCompletions(languageId, text, prefix.length + (at < 0 ? typed.length : at), opts);
  if (!list) return null;
  assert.equal(list.isIncomplete, true);
  const out = {};
  for (const item of list.items) {
    const { start, end } = item.textEdit.range;
    assert.equal(start.line, end.line, item.label);
    out[item.label] = item.textEdit.newText;
  }
  return out;
}
const markup = header + '<main>\n  ';

// Expansions.
assert.equal(
  expand(markup, 'div.card>ul>li*3')['div.card>ul>li*3'],
  '<div class="card">\n\t<ul>\n\t\t<li>${1}</li>\n\t\t<li>${2}</li>\n\t\t<li>${0}</li>\n\t</ul>\n</div>',
);
assert.equal(expand(markup, '.foo')['.foo'], '<div class="foo">${0}</div>');
assert.equal(expand(markup, 'ul>li')['ul>li'], '<ul>\n\t<li>${0}</li>\n</ul>');
// After indentation, inside elements, after a tag or a hole on the line,
// after a statement line, in the script form.
assert.equal(expand(markup, '<section>\n    <div>\n      .foo')['.foo'], '<div class="foo">${0}</div>');
assert.equal(expand(markup, '<p>.foo')['.foo'], '<div class="foo">${0}</div>');
assert.equal(expand(markup, '<p>.foo|</p>')['.foo'], '<div class="foo">${0}</div>');
assert.equal(expand(markup, '<components.Card />.foo')['.foo'], '<div class="foo">${0}</div>');
assert.equal(expand(markup, '<ui.Button size="sm" onClick={Save()}>.foo')['.foo'], '<div class="foo">${0}</div>');
assert.equal(expand(markup, '<p>{Q}.foo')['.foo'], '<div class="foo">${0}</div>');
assert.equal(expand(markup, '<!-- note -->.foo')['.foo'], '<div class="foo">${0}</div>');
assert.equal(expand(markup, 'if editing {\n    .foo')['.foo'], '<div class="foo">${0}</div>');
assert.equal(expand(markup, '<input/>\n  .foo')['.foo'], '<div class="foo">${0}</div>');
assert.equal(expand(markup, 'slot:actions')['slot:actions'], '<slot:actions>${0}</slot:actions>');
assert.equal(expand(markup, 'div.Card')['div.Card'], '<div class="Card">${0}</div>');
const scriptMarkup = script.slice(0, script.indexOf('<main class')) + '<main>\n  ';
assert.equal(expand(scriptMarkup, '.foo')['.foo'], '<div class="foo">${0}</div>');
// The range replaces just the abbreviation.
{
  const text = markup + '<p>.foo';
  const [item] = emmetCompletions('vuka-html', text, text.length, config).items;
  const lines = text.split('\n');
  assert.deepEqual(item.textEdit.range, {
    start: { line: lines.length - 1, character: lines.at(-1).length - 4 },
    end: { line: lines.length - 1, character: lines.at(-1).length },
  });
}

// Nothing in the Go header: every line of it, and words typed after it.
for (const doc of [fixture, script]) {
  const head = doc.slice(0, doc.indexOf('<main class'));
  for (let end = 1; end <= head.length; end++) {
    assert.equal(emmetCompletions('vuka-html', doc, end, config), undefined, `header at ${end}: ${JSON.stringify(head.slice(Math.max(0, end - 20), end))}`);
  }
}
for (const typed of ['div', '.foo', 'ul>li', 'p.x']) {
  assert.equal(expand('package pages\n\ntype P struct{}\n\n', typed), null, typed);
  assert.equal(expand('<script lang="go">\npackage pages\n', typed), null, typed);
}
// Nowhere but in a text node of the markup.
for (const typed of [
  '<p>{Q.foo',                              // a Go hole
  '<p>{"ul>li',                             // a string in a hole
  '<p>{/* ul>li',                           // a comment in a hole
  '<div class="ul>li',                      // an attribute value
  '<div hidden ul',                         // a tag's attributes
  '<ui.Button size="sm" ',                  // a component tag
  '<components.Card ',
  '<button onClick={Save(); .foo',
  '<!-- ul>li',                             // an HTML comment
  '<script lang="ts">\nconst x = 1\nul>li', // a script
  '<style>\n.foo',                          // a style
  'if open {',                              // statement lines
  'for _, t := range rows {',
  'x := .foo',
  '</',                                     // a closing tag
  '<',
]) {
  assert.equal(expand(markup, typed), null, typed);
}
// A component's name is a tag, not a tag and a class.
for (const typed of ['ui.DataTable', 'components.Item', 'ul>ui.Button', '<p>ui.Card']) {
  assert.equal(expand(markup, typed), null, typed);
}
// Other languages, and settings turning Emmet off.
assert.equal(expand(markup, '.foo', config, 'vuka'), null);
assert.equal(expand(markup, '.foo', config, 'html'), null);
assert.equal(expand(markup, '.foo', { ...config, showExpandedAbbreviation: 'never' }), null);
assert.equal(expand(markup, '.foo', { ...config, excludeLanguages: ['vuka-html'] }), null);

// textNodeStart is where the text before the cursor starts.
assert.equal(textNodeStart('<main>\n  <p>{Q}ab'), '<main>\n  <p>{Q}'.length);
assert.equal(textNodeStart('<main>\n  if x {\n  ab'), '<main>\n  if x {\n'.length);
assert.equal(textNodeStart('<main><!-- c -->ab'), '<main><!-- c -->'.length);
assert.equal(textNodeStart('<main ab'), -1);

// The provider is registered on these characters; / is the closing tag's.
assert.ok(triggerCharacters.includes('.') && triggerCharacters.includes('>') && triggerCharacters.includes('*'));
assert.ok(!triggerCharacters.includes('/'));
assert.equal(pkg.dependencies['@vscode/emmet-helper'] !== undefined, true);
console.log('emmet: ok');
