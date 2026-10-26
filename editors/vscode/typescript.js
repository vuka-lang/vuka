// The TypeScript server for <script lang="ts"> blocks: vuka lsp hands each
// script to a typescript-language-server. The extension ships one, runs it
// with VS Code's own Node (its Electron binary, as Node through
// ELECTRON_RUN_AS_NODE) and gives it the TypeScript inside VS Code, which
// vuka lsp uses when the module has no typescript of its own. Nothing to
// install, on any platform; vuka.typescript.server names another server.
const path = require('path');
const fs = require('fs');

const isJS = (file) => /\.(c|m)?js$/i.test(file);

// bundledServer is the typescript-language-server in the extension.
function bundledServer(extensionPath, exists = fs.existsSync) {
  const cli = path.join(extensionPath, 'node_modules', 'typescript-language-server', 'lib', 'cli.mjs');
  return exists(cli) ? cli : undefined;
}

// vscodeTypeScript is the lib directory of the TypeScript VS Code ships:
// beside its built-in extensions (appRoot/extensions/node_modules), or in
// or beside the TypeScript features extension.
function vscodeTypeScript(appRoot, builtinExtensionPath, exists = fs.existsSync) {
  const candidates = [];
  if (appRoot) candidates.push(path.join(appRoot, 'extensions', 'node_modules', 'typescript', 'lib'));
  if (builtinExtensionPath) {
    candidates.push(path.join(builtinExtensionPath, 'node_modules', 'typescript', 'lib'));
    candidates.push(path.join(builtinExtensionPath, '..', 'node_modules', 'typescript', 'lib'));
  }
  return candidates.find((dir) => exists(path.join(dir, 'tsserver.js')));
}

// launcher is the server vuka lsp runs: { server, node, tsserver }, or
// undefined when there is none. setting is vuka.typescript.server: a server
// of the user's (an executable, or a .js/.mjs file VS Code's Node runs).
function launcher({ setting, extensionPath, execPath, appRoot, builtinExtensionPath, exists = fs.existsSync }) {
  const server = setting || bundledServer(extensionPath, exists);
  if (!server) return undefined;
  return {
    server,
    node: isJS(server) ? execPath : undefined,
    tsserver: vscodeTypeScript(appRoot, builtinExtensionPath, exists),
  };
}

// lspArgs are vuka lsp's flags for l (vuka 2.5.1 and newer).
function lspArgs(l) {
  if (!l) return [];
  const args = ['-tsls', l.server];
  if (l.node) args.push('-tsls-node', l.node);
  if (l.tsserver) args.push('-tsserver', l.tsserver);
  return args;
}

// lspEnv is l as the environment vuka lsp also reads.
function lspEnv(l) {
  if (!l) return {};
  const env = { VUKA_TSLS: l.server };
  if (l.node) env.VUKA_TSLS_NODE = l.node;
  if (l.tsserver) env.VUKA_TSSERVER = l.tsserver;
  return env;
}

module.exports = { bundledServer, vscodeTypeScript, launcher, lspArgs, lspEnv, isJS };
