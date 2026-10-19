// .vuka.html grammar test: tokenizes test/fixtures-html/*.vuka.html and checks
// the scopes of its holes, statements, header, script and style.
import { readFileSync } from 'node:fs';
import { createHighlighter } from 'shiki';

const read = (p) => JSON.parse(readFileSync(new URL(p, import.meta.url), 'utf8'));
const html = read('../syntaxes/vuka-html.tmLanguage.json');
const inject = read('../syntaxes/vuka-html-injection.tmLanguage.json');
const highlighter = await createHighlighter({
  themes: ['github-dark'],
  langs: ['go', 'typescript', 'css', 'html',
    { ...html, name: 'vuka-html', embeddedLangs: ['go', 'typescript', 'css', 'html'] },
    { ...inject, name: 'vuka-html-injection', injectTo: ['source.go'], embeddedLangs: ['vuka-html'] }],
});

let failures = 0;
function scopesAt(code, lang, needle) {
  const at = code.indexOf(needle);
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

const go = 'package p\n\nfunc row(o Order) ui.Node {\n\treturn ui.HTML(`<li key={o.ID}>{o.Ref}</li>`, o.ID, o.Ref)\n}\n';
expect(go, 'go', '<li', 'punctuation.definition.tag');
expect(go, 'go', 'o.Ref}', 'source.go');

if (failures) process.exit(1);
console.log('ok html grammar');
