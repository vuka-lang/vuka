// Locate the Go/markup boundary for editing features. Keep lexical state for
// strings, comments and balanced Go delimiters; a tag-looking raw-string line
// or a receive expression in a method must never start tag completion.
function markupStart(text) {
  let depth = 0;
  let lineStart = true;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (c === '\n') { lineStart = true; continue; }
    if (lineStart && (c === ' ' || c === '\t' || c === '\r' || c === '\uFEFF')) continue;
    if (lineStart && depth === 0 && c === '<' && text[i + 1] !== '-') return i;
    lineStart = false;
    if (c === '/' && text[i + 1] === '/') {
      const end = text.indexOf('\n', i + 2);
      if (end < 0) return -1;
      i = end - 1;
    } else if (c === '/' && text[i + 1] === '*') {
      const end = text.indexOf('*/', i + 2);
      if (end < 0) return -1;
      i = end + 1;
    } else if (c === '"' || c === "'" || c === '`') {
      const quote = c;
      let closed = false;
      while (++i < text.length) {
        if (text[i] === '\\' && quote !== '`') i++;
        else if (text[i] === quote) { closed = true; break; }
      }
      if (!closed) return -1;
    } else if ('{(['.includes(c)) depth++;
    else if ('})]'.includes(c)) depth = Math.max(0, depth - 1);
  }
  return -1;
}

function markupBefore(document, text) {
  if (document.languageId !== 'vuka-html') return null;
  const start = markupStart(text);
  return start < 0 ? null : text.slice(start);
}

const { readTag, voidElements } = require('./jsx');

// Ignore Go holes and literal script/style content when looking for open tags.
// The legacy JSX helper remains available for tolerant grammar tests, while
// shape editing scans only the markup half of a file.
function tagsInMarkup(text) {
  const tags = [];
  for (let i = 0; i < text.length; i++) {
    if (i === 0 || text[i - 1] === '\n') {
      const statement = /^[ \t]*(?:\}\s*)?(?:(?:if|for|switch|else|case|default|match)\b|[A-Za-z_][\w., \t]*:?=[^=])/.exec(text.slice(i));
      if (statement) {
        const end = text.indexOf('\n', i);
        if (end < 0) return tags;
        i = end;
        continue;
      }
    }
    if (text.startsWith('<!--', i)) {
      const end = text.indexOf('-->', i + 4);
      if (end < 0) return tags;
      i = end + 2;
    } else if (text[i] === '{' && text[i + 1] === '{') {
      i++;
    } else if (text[i] === '{') {
      let depth = 1;
      while (++i < text.length && depth) {
        if (text[i] === '/' && text[i + 1] === '/') {
          const end = text.indexOf('\n', i + 2);
          if (end < 0) return tags;
          i = end;
        } else if (text[i] === '/' && text[i + 1] === '*') {
          const end = text.indexOf('*/', i + 2);
          if (end < 0) return tags;
          i = end + 1;
        } else if ('"\'`'.includes(text[i])) {
          const quote = text[i];
          while (++i < text.length) {
            if (text[i] === '\\' && quote !== '`') i++;
            else if (text[i] === quote) break;
          }
        } else if (text[i] === '{') depth++;
        else if (text[i] === '}') depth--;
      }
      i--;
    } else if (text[i] === '<') {
      const tag = readTag(text, i);
      if (!tag) continue;
      tags.push(tag);
      i = tag.end - 1;
      if (!tag.close && !tag.self && /^(script|style)$/.test(tag.name)) {
        const end = text.indexOf(`</${tag.name}`, i + 1);
        if (end < 0) return tags;
        i = end - 1;
      }
    }
  }
  return tags;
}

function tagToClose(text) {
  const tag = tagsInMarkup(text).at(-1);
  return tag && tag.end === text.length && !tag.close && !tag.self && !voidElements.has(tag.name)
    ? tag.name : null;
}

function closingFor(text, after) {
  if (!text.endsWith('</') || /^[\w.:-]/.test(after)) return null;
  // Scanning through the partial closer lets an unfinished Go hole/comment
  // or script body suppress it; append a sentinel to test that lexical state.
  const probeText = text.slice(0, -2) + '<vuka-closer-probe/>';
  const tags = tagsInMarkup(probeText);
  const last = tags.pop();
  if (!last || last.end !== probeText.length || last.name !== 'vuka-closer-probe') return null;
  const stack = [];
  for (const tag of tags) {
    if (tag.close) {
      const index = stack.lastIndexOf(tag.name);
      if (index >= 0) stack.length = index;
    } else if (!tag.self && !voidElements.has(tag.name)) stack.push(tag.name);
  }
  const name = stack.at(-1);
  return name === undefined ? null : name + (after.startsWith('>') ? '' : '>');
}

module.exports = { markupStart, markupBefore, tagToClose, closingFor };
