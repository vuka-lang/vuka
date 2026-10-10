// Unit tests of the extension's JSX logic (jsx.js) and of the editing rules
// in language-configuration.json, typing the benchmark page of
// test/fixtures/benchmark.vuka the way the editor would.
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const { tagToClose, unclosedTag, closingFor } = require('../jsx.js');
const config = JSON.parse(readFileSync(new URL('../language-configuration.json', import.meta.url), 'utf8'));
const page = readFileSync(new URL('./fixtures/benchmark.vuka', import.meta.url), 'utf8');

let failures = 0;
let checked = 0;
function eq(what, got, want) {
  checked++;
  if (got !== want) {
    failures++;
    console.error(`FAIL ${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  }
}

// upTo is the page up to the nth (1-based) occurrence of needle, and needle.
function upTo(needle, n = 1) {
  let i = -1;
  for (let k = 0; k < n; k++) i = page.indexOf(needle, i + 1);
  if (i < 0) throw new Error(`no ${needle}`);
  return page.slice(0, i + needle.length);
}

// > closes the tag it finishes: first on a line, after another tag on the
// line, in a block's body, a fragment; never a void, self-closing or Go <.
for (const [before, want] of [
  [upTo('return <>'), ''],
  [upTo('<h1>'), 'h1'],
  [upTo('<table>'), 'table'],
  [upTo('<tr>'), 'tr'],
  [upTo('<tr><th>'), 'th'],
  [upTo('</th><th>'), 'th'],
  [upTo('pets {\n\t\t\t\t<tr>'), 'tr'],
  [upTo('<td>'), 'td'],
  [upTo('p.ID)}>'), 'a'],
  [upTo('<td>', 3), 'td'],
  ['\treturn <input type="text">', null],
  ['\treturn <br>', null],
  ['\treturn <Card />', null],
  ['\treturn <List[User] items={us}>', 'List'],
  ['\treturn <theme.Card>', 'theme.Card'],
  ['\treturn <b onClick={func() { if a > b { x() } }}>', 'b'],
  ['\treturn <p title="a > b">', 'p'],
  ['\tif a < b && c >', null],
  ['\tx := y<b>', null],
  ['\treturn <p>{a >', null],
  ['\treturn <p title={a >', null],
  ['\t</td>', null],
]) {
  eq(`tagToClose(…${JSON.stringify(before.slice(-30))})`, tagToClose(before), want);
}

// </ completes the innermost element still open, through blocks.
for (const [needle, n, want] of [
  ['<h1>Pets ({len(pets)})<', 1, 'h1'],
  ['<th>Name<', 1, 'th'],
  ['</th></tr>\n\t\t\t{for _, p := range pets {\n\t\t\t\t<tr>\n\t\t\t\t\t<td>\n\t\t\t\t\t\t<a href={fmt.Sprintf("/pets/%d", p.ID)}>{p.Name}<', 1, 'a'],
  ['{p.Name}</a>\n\t\t\t\t\t<', 1, 'td'],
  ['<td>{p.Age}</td>\n\t\t\t\t<', 1, 'tr'],
  ['}}\n\t\t<', 1, 'table'],
  ['</table>\n\t<', 1, ''],
]) {
  const before = upTo(needle, n);
  eq(`closingFor(…${JSON.stringify(before.slice(-25))}/)`, closingFor(before + '/', '\n'), want + '>');
}
eq('closingFor before an existing >', closingFor('\treturn <p>x</', '>'), 'p');
eq('closingFor before a name', closingFor('\treturn <p>x</', 'p>'), null);
eq('closingFor outside markup', closingFor('\tx := 1 </', ''), null);
eq('unclosedTag skips Go strings', unclosedTag('\ts := "<b>"\n\treturn <i>'), 'i');
eq('unclosedTag none', unclosedTag('\treturn <p>x</p>'), null);

// Enter: between a tag and its closer the closer goes on its own line; after
// an opening tag (also after another on the line) or a block's {, indent;
// a closer outdents.
const re = (s) => new RegExp(s);
const [between, after] = config.onEnterRules;
for (const line of ['\t\t\t\t<tr>', '\treturn <>', '\t\t\t<tr><td>', '\t\t\t\t\t\t<a href={fmt.Sprintf("/x/%d", p.ID)}>', '\t\t<List[User] items={us}>']) {
  eq(`onEnter before ${JSON.stringify(line)}`, re(between.beforeText).test(line) && re(after.beforeText).test(line), true);
  eq(`increaseIndent ${JSON.stringify(line)}`, re(config.indentationRules.increaseIndentPattern).test(line), true);
}
eq('onEnter afterText', re(between.afterText).test('</td>'), true);
for (const line of ['\t<br />', '\t<input type="text" />', '\tif a < b {x', '\tx := 1']) {
  eq(`no tag indent after ${JSON.stringify(line)}`, re(after.beforeText).test(line), false);
}
eq('increaseIndent after {for', re(config.indentationRules.increaseIndentPattern).test('\t\t\t{for _, p := range pets {'), true);
for (const line of ['\t\t\t\t</tr>', '\t\t\t}}', '\t</>']) {
  eq(`decreaseIndent ${JSON.stringify(line)}`, re(config.indentationRules.decreaseIndentPattern).test(line), true);
}
// { and " pair before a closing tag and a tag's >, as in <td>{|}</td>.
eq('autoCloseBefore has <', config.autoCloseBefore.includes('<'), true);
eq('autoCloseBefore has >', config.autoCloseBefore.includes('>'), true);
eq('" pairs', config.autoClosingPairs.some((p) => p.open === '"'), true);

console.log(`${checked} checks, ${failures} failures`);
process.exit(failures ? 1 : 0);
