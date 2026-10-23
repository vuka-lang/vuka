// The routes a code lens above a page shows: the language server sends each
// route's `vuka routes -json` entry as the lens's arguments.

const tokenTypes = ['vukaProp', 'vukaState', 'vukaBrowserState', 'vukaEvent', 'vukaComponent', 'vukaLocal', 'vukaContextVar'];

// formatRoute is one entry as lines of text.
function formatRoute(e) {
  const lines = [`${e.method} ${e.path}`, `  page        ${e.page} (${e.kind})`, `  declared    ${e.file}:${e.line}`];
  const list = (label, v) => {
    if (v && v.length) lines.push(`  ${label.padEnd(11)} ${v.join(', ')}`);
  };
  list('middleware', e.middleware);
  list('guards', e.guards);
  list('layouts', e.layouts);
  if (e.root) lines.push(`  root        ${e.root}`);
  return lines.join('\n');
}

// moduleRoot is the directory holding go.mod at or above dir, undefined when none does.
function moduleRoot(dir, exists) {
  for (let d = dir; ; ) {
    if (exists(d + '/go.mod')) return d;
    const up = d.replace(/\/[^/]*$/, '');
    if (up === d || up === '') return undefined;
    d = up;
  }
}

module.exports = { tokenTypes, formatRoute, moduleRoot };
