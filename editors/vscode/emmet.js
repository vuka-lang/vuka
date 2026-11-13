// Emmet abbreviations in .vuka.html markup, apart from VS Code so node can
// test it. @vscode/emmet-helper expands them, as it does for HTML; this
// decides where: only in a text node of the markup half of a shape, never
// in the Go header, a Go hole, a tag's attributes, a comment, a script or
// style body or a statement line.
const { doComplete } = require('@vscode/emmet-helper');
const { TextDocument } = require('vscode-languageserver-textdocument');
const { htmlData } = require('@vscode/emmet-helper/lib/cjs/data');
const { markupStart, textNodeStart } = require('./shape');

// The characters built-in Emmet asks for completions on in HTML, except /,
// which starts a closing tag the extension finishes itself.
const triggerCharacters = ['!', '.', '}', ':', '*', '$', ']', '>', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9'];

// A qualified component's name (ui.DataTable, components.Item) is a tag in
// a shape, where Emmet would read a tag and a class: an element named by
// something other than an HTML tag, then a dot and a capital.
const htmlTags = new Set(htmlData.tags);
function namesComponent(abbreviation) {
  for (const m of abbreviation.matchAll(/(?:^|[>+^(])([A-Za-z_]\w*)\.[A-Z]/g)) {
    if (!htmlTags.has(m[1])) return true;
  }
  return false;
}

// emmetCompletions is the CompletionList (LSP types) of the Emmet
// abbreviation that ends at offset, or undefined where none expands.
// config holds VS Code's emmet.* settings.
function emmetCompletions(languageId, text, offset, config = {}) {
  if (languageId !== 'vuka-html' || (config.excludeLanguages || []).includes('vuka-html')) return undefined;
  const markupAt = markupStart(text);
  if (markupAt < 0 || offset < markupAt) return undefined;
  const markup = text.slice(markupAt, offset);
  const start = textNodeStart(markup);
  if (start < 0) return undefined;
  // Emmet looks for the abbreviation on the line; what comes before the
  // text node on it (a component tag, a Go hole) is blanked so Emmet never
  // reads it as part of one.
  const from = offset - markup.length + start;
  const line = text.lastIndexOf('\n', offset - 1) + 1;
  const blanked = from > line ? text.slice(0, line) + ' '.repeat(from - line) + text.slice(from) : text;
  const document = TextDocument.create('untitled:shape.vuka.html', 'html', 0, blanked);
  const list = doComplete(document, document.positionAt(offset), 'html', config);
  if (!list) return undefined;
  const items = list.items.filter((item) => !namesComponent(item.label.split('|')[0]));
  return items.length ? { ...list, items } : undefined;
}

module.exports = { emmetCompletions, triggerCharacters };
