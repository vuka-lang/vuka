// The extension's reading of JSX in .vuka text, apart from VS Code so node
// can test it: which tag a typed > finishes, and which element a typed </
// closes.

const voidElements = new Set(['area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link', 'meta', 'source', 'track', 'wbr']);
const nameAt = /^[A-Za-z_][\w.:-]*/;
const isSpace = (c) => c === ' ' || c === '\t' || c === '\n' || c === '\r';

// skipQuoted is the offset after the string at i, -1 when it isn't closed.
function skipQuoted(s, i) {
  const q = s[i];
  for (let j = i + 1; j < s.length; j++) {
    if (s[j] === '\\' && q !== '`') j++;
    else if (s[j] === q) return j + 1;
    else if (s[j] === '\n' && q !== '`') return -1;
  }
  return -1;
}

// skipBraces is the offset after the } closing the { at i, -1 when none does.
function skipBraces(s, i) {
  let depth = 0;
  for (let j = i; j < s.length; j++) {
    const c = s[j];
    if (c === '{') depth++;
    else if (c === '}' && --depth === 0) return j + 1;
    else if (c === '"' || c === "'" || c === '`') {
      const e = skipQuoted(s, j);
      if (e < 0) return -1;
      j = e - 1;
    }
  }
  return -1;
}

// readTag reads a whole tag at the < at i: { name, close, self, end }, or
// null when there is none, or not all of it yet. A fragment is <> or </>.
function readTag(s, i) {
  let j = i + 1;
  const close = s[j] === '/';
  if (close) j++;
  const m = nameAt.exec(s.slice(j, j + 200));
  const name = m ? m[0] : '';
  j += name.length;
  if (name && !close && s[j] === '[') {
    const e = s.indexOf(']', j);
    if (e < 0 || /[\n<>]/.test(s.slice(j, e))) return null;
    j = e + 1;
  }
  if (!name && s[j] !== '>') return null;
  for (;;) {
    while (j < s.length && isSpace(s[j])) j++;
    if (j >= s.length) return null;
    const c = s[j];
    if (c === '>') return { name, close, self: false, end: j + 1 };
    if (c === '/' && s[j + 1] === '>' && !close) return { name, close, self: true, end: j + 2 };
    if (close) return null;
    if (c === '{') {
      j = skipBraces(s, j);
      if (j < 0) return null;
      continue;
    }
    const a = nameAt.exec(s.slice(j, j + 200));
    if (!a) return null;
    j += a[0].length;
    let k = j;
    while (k < s.length && isSpace(s[k])) k++;
    if (s[k] !== '=') continue;
    k++;
    while (k < s.length && isSpace(s[k])) k++;
    if (s[k] === '"' || s[k] === "'") j = skipQuoted(s, k);
    else if (s[k] === '{') j = skipBraces(s, k);
    else return null;
    if (j < 0) return null;
  }
}

// opensMarkup reports whether a < at i can start markup in Go: after what
// takes an operand (return ( , = { : [ ;), after a tag or a {…} child, or
// first on its line. Inside markup any tag-shaped < is one.
function opensMarkup(s, i, inMarkup) {
  const before = s[i - 1];
  if (before !== undefined && (/[\w)\]]/.test(before) && !/return$/.test(s.slice(0, i)))) return false;
  if (inMarkup) return true;
  const prev = s.slice(Math.max(0, i - 200), i).replace(/[ \t]+$/, '');
  return prev === '' || /(?:[\n(,={:[;>}]|\breturn)$/.test(prev);
}

// tagToClose is the name of the opening tag a > typed at the end of before
// finishes ('' for a fragment), or null: not a tag, a self-closing or void one.
function tagToClose(before) {
  if (!before.endsWith('>')) return null;
  const floor = Math.max(0, before.length - 4000);
  for (let i = before.lastIndexOf('<'), n = 0; i >= floor && n < 64; i = before.lastIndexOf('<', i - 1), n++) {
    const t = readTag(before, i);
    if (!t || t.end !== before.length) continue;
    if (t.close || t.self || voidElements.has(t.name) || !opensMarkup(before, i, t.name !== '')) return null;
    return t.name;
  }
  return null;
}

// unclosedTag is the innermost element still open at the end of before, ''
// for a fragment, null when none is.
function unclosedTag(before) {
  const s = before.length > 200000 ? before.slice(-200000) : before;
  const stack = [];
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (!stack.length && (c === '"' || c === '`' || c === "'")) {
      const e = skipQuoted(s, i);
      if (e > 0) i = e - 1;
      continue;
    }
    if (!stack.length && c === '/' && s[i + 1] === '/') {
      const e = s.indexOf('\n', i);
      i = e < 0 ? s.length : e;
      continue;
    }
    if (c !== '<') continue;
    const t = readTag(s, i);
    if (!t) continue;
    if (t.close) {
      const k = stack.lastIndexOf(t.name);
      if (k >= 0) stack.length = k;
    } else if (opensMarkup(s, i, stack.length > 0)) {
      if (!t.self && !voidElements.has(t.name)) stack.push(t.name);
    } else {
      continue;
    }
    i = t.end - 1;
  }
  return stack.length ? stack[stack.length - 1] : null;
}

// closingFor is what typing / after < should add: the open element's name and
// its >, or null. after is the text after the cursor.
function closingFor(before, after) {
  if (!before.endsWith('</') || /^[\w.:-]/.test(after)) return null;
  const name = unclosedTag(before.slice(0, -2));
  if (name === null || (name === '' && after.startsWith('>'))) return null;
  return after.startsWith('>') ? name : name + '>';
}

module.exports = { voidElements, readTag, tagToClose, unclosedTag, closingFor };
