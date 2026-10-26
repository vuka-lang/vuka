// Keep editor requirements explicit: updating the extension does not update
// the separately installed language server.
const minimumServerVersion = 'v2.0.0';
const updateCommand = `go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest`;

function serverVersion(output) {
  const match = /^vuka v(\d+)\.(\d+)\.(\d+)(?=[\s+-]|$)/m.exec(output);
  if (!match) return undefined;
  return { text: `v${match[1]}.${match[2]}.${match[3]}`, parts: match.slice(1, 4).map(Number) };
}

function supportsShapes(output) {
  const version = serverVersion(output);
  if (!version) return undefined;
  return version.parts[0] >= 2;
}

// supportsTSFlags reports whether vuka lsp takes -tsls, -tsls-node and
// -tsserver (v2.5.1); false when the version can't be read.
function supportsTSFlags(output) {
  const version = serverVersion(output);
  if (!version) return false;
  const [a, b, c] = version.parts;
  return a > 2 || (a === 2 && (b > 5 || (b === 5 && c >= 1)));
}

module.exports = { minimumServerVersion, updateCommand, serverVersion, supportsShapes, supportsTSFlags };
