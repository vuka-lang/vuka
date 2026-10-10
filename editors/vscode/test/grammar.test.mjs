// Grammar regression test: tokenizes test/fixtures/*.vuka with the extension's
// grammar on top of Go's (shiki's TextMate engine and oniguruma, as VS Code
// and the docs site run it), checks the scopes at key positions, and compares
// every token's scopes with test/fixtures/*.vuka.snap.
//
//   npm test                  check
//   npm test -- --update      rewrite the snapshots
import { readFileSync, writeFileSync, readdirSync, existsSync } from 'node:fs';
import { createHighlighter } from 'shiki';
import { assertions } from './assertions.mjs';

const dir = new URL('./fixtures/', import.meta.url);
const grammar = JSON.parse(readFileSync(new URL('../syntaxes/vuka.tmLanguage.json', import.meta.url), 'utf8'));
const update = process.argv.includes('--update');

const highlighter = await createHighlighter({
  themes: ['github-dark'],
  langs: ['go', { ...grammar, name: 'vuka', embeddedLangs: ['go'] }],
});

// tokenize is each line's tokens: their text and scopes, outermost first,
// source.vuka left out.
function tokenize(code) {
  const lines = highlighter.codeToTokensBase(code, { lang: 'vuka', theme: 'github-dark', includeExplanation: 'scopeName' });
  return lines.map((tokens) =>
    tokens.flatMap((t) =>
      t.explanation.map((e) => ({
        text: e.content,
        scopes: e.scopes.map((s) => s.scopeName).filter((s) => s !== 'source.vuka'),
      })),
    ),
  );
}

function snapshot(lines) {
  const out = [];
  lines.forEach((tokens, i) => {
    out.push(`${i + 1}:`);
    for (const t of tokens) {
      if (t.text.trim() === '') continue;
      out.push(`  ${JSON.stringify(t.text)} ${t.scopes.join(' ') || '-'}`);
    }
  });
  return out.join('\n') + '\n';
}

let failures = 0;
function fail(msg) {
  failures++;
  console.error('FAIL ' + msg);
}

const files = readdirSync(dir).filter((f) => f.endsWith('.vuka')).sort();
const tokens = {};
for (const f of files) {
  const code = readFileSync(new URL(f, dir), 'utf8');
  tokens[f] = { code: code.split('\n'), lines: tokenize(code) };
  const snapFile = new URL(f + '.snap', dir);
  const got = snapshot(tokens[f].lines);
  if (update || !existsSync(snapFile)) {
    writeFileSync(snapFile, got);
  } else if (readFileSync(snapFile, 'utf8') !== got) {
    const want = readFileSync(snapFile, 'utf8').split('\n');
    const have = got.split('\n');
    const i = have.findIndex((l, n) => l !== want[n]);
    fail(`${f}.snap differs (first at snapshot line ${i + 1}):\n  want ${want[i]}\n  got  ${have[i]}\n  (npm test -- --update rewrites it)`);
  }
}

// An assertion: in file, on line (1-based), the nth (default 1st) occurrence
// of text; every token it covers has a scope starting with scope, or, with
// not: true, none has.
let checked = 0;
for (const a of assertions) {
  const { file, line, text, scope } = a;
  const t = tokens[file];
  const where = `${file}:${line} ${JSON.stringify(text)}`;
  if (!t) {
    fail(`${where}: no fixture ${file}`);
    continue;
  }
  const src = t.code[line - 1] ?? '';
  let col = -1;
  for (let n = 0; n < (a.nth || 1); n++) col = src.indexOf(text, col + 1);
  if (col < 0) {
    fail(`${where}: not on the line: ${src}`);
    continue;
  }
  let pos = 0;
  const covered = [];
  for (const tok of t.lines[line - 1]) {
    const end = pos + tok.text.length;
    if (end > col && pos < col + text.length) covered.push(tok);
    pos = end;
  }
  const has = (tok) => tok.scopes.some((s) => s === scope || s.startsWith(scope + '.') || s.split(' ').includes(scope));
  const bad = covered.filter((tok) => (a.not ? has(tok) : !has(tok)));
  checked++;
  if (bad.length) {
    fail(`${where}: ${a.not ? 'unexpected' : 'want'} ${scope}; got ${bad.map((b) => `${JSON.stringify(b.text)} [${b.scopes.join(' ')}]`).join(', ')}`);
  }
}

// A fixture, and every vuka sample on the docs site (which highlights with
// this grammar), must close what it opens: a line after it is plain Go again.
function leftOpen(code) {
  const lines = tokenize(code.replace(/\n?$/, '\n') + 'var sentinel = 1\n');
  const last = lines[lines.length - 2] ?? [];
  return last.find((t) => t.text === 'sentinel')?.scopes.filter((s) => s !== 'variable.other.assignment.go') ?? ['?'];
}
for (const f of files) {
  const open = leftOpen(tokens[f].code.join('\n'));
  if (open.length) fail(`${f}: leaves ${open.join(' ')} open`);
}

const docs = new URL('../../../docs/', import.meta.url);
let samples = 0;
function walk(d) {
  for (const e of readdirSync(d, { withFileTypes: true })) {
    if (e.name.startsWith('.') || e.name === 'node_modules') continue;
    const u = new URL(e.name + (e.isDirectory() ? '/' : ''), d);
    if (e.isDirectory()) walk(u);
    else if (e.name.endsWith('.md')) {
      const md = readFileSync(u, 'utf8');
      for (const m of md.matchAll(/^([ \t]*)```vuka\n([\s\S]*?)^\1```/gm)) {
        samples++;
        const open = leftOpen(m[2].replace(new RegExp('^' + m[1], 'gm'), ''));
        if (open.length) {
          const line = md.slice(0, m.index).split('\n').length;
          fail(`docs/${u.pathname.split('/docs/')[1]}:${line}: sample leaves ${open.join(' ')} open`);
        }
      }
    }
  }
}
if (existsSync(docs)) walk(docs);

console.log(`${files.length} fixtures, ${checked} assertions, ${samples} docs samples, ${failures} failures`);
process.exit(failures ? 1 : 0);
