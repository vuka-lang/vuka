// Keep editor requirements explicit: updating the extension does not update
// the separately installed language server.
const minimumServerVersion = 'v1.4.0';
const updateCommand = `go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest`;

function serverVersion(output) {
  const match = /^vuka v(\d+)\.(\d+)\.(\d+)(?=[\s+-]|$)/m.exec(output);
  if (!match) return undefined;
  return { text: `v${match[1]}.${match[2]}.${match[3]}`, parts: match.slice(1, 4).map(Number) };
}

function supportsTemplateContext(output) {
  const version = serverVersion(output);
  if (!version) return undefined;
  const [major, minor] = version.parts;
  return major > 1 || (major === 1 && minor >= 4);
}

module.exports = { minimumServerVersion, updateCommand, serverVersion, supportsTemplateContext };
