// The TypeScript server the extension hands vuka lsp: the bundled
// typescript-language-server run by VS Code's Node with VS Code's
// TypeScript. With $VUKA_BIN (a vuka binary) and gopls, it also runs vuka
// lsp the way the extension does, no TypeScript server on PATH, and asks
// for completions in a page's script and a component's.
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { cpSync, existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const require = createRequire(import.meta.url);
const typescript = require('../typescript.js');
const toolchain = require('../toolchain.js');
const here = path.dirname(fileURLToPath(import.meta.url));
const ext = path.join(here, '..');

// The launcher: the bundled server, VS Code's Node, VS Code's TypeScript.
const appRoot = path.join('/vscode', 'resources', 'app');
const vscodeLib = path.join(appRoot, 'extensions', 'node_modules', 'typescript', 'lib');
const fakeExists = (files) => (p) => files.includes(p);
const bundled = path.join(ext, 'node_modules', 'typescript-language-server', 'lib', 'cli.mjs');
let l = typescript.launcher({ extensionPath: ext, execPath: '/vscode/Code Helper', appRoot, exists: fakeExists([bundled, path.join(vscodeLib, 'tsserver.js')]) });
assert.deepEqual(l, { server: bundled, node: '/vscode/Code Helper', tsserver: vscodeLib });
assert.deepEqual(typescript.lspArgs(l), ['-tsls', bundled, '-tsls-node', '/vscode/Code Helper', '-tsserver', vscodeLib]);
assert.deepEqual(typescript.lspEnv(l), { VUKA_TSLS: bundled, VUKA_TSLS_NODE: '/vscode/Code Helper', VUKA_TSSERVER: vscodeLib });
// The TypeScript features extension's own copy, when appRoot has none.
const builtin = path.join('/vscode', 'extensions', 'typescript-language-features');
const besideBuiltin = path.join(builtin, '..', 'node_modules', 'typescript', 'lib');
assert.equal(typescript.vscodeTypeScript(undefined, builtin, fakeExists([path.join(besideBuiltin, 'tsserver.js')])), besideBuiltin);
// vuka.typescript.server: an executable runs as itself, a script with VS Code's Node.
l = typescript.launcher({ setting: '/usr/bin/typescript-language-server', extensionPath: ext, execPath: '/node', exists: fakeExists([]) });
assert.deepEqual(typescript.lspArgs(l), ['-tsls', '/usr/bin/typescript-language-server']);
l = typescript.launcher({ setting: 'C:\\tools\\tsls\\lib\\cli.mjs', extensionPath: ext, execPath: 'C:\\VS Code\\Code.exe', exists: fakeExists([]) });
assert.equal(l.node, 'C:\\VS Code\\Code.exe');
assert.equal(typescript.launcher({ extensionPath: '/nowhere', exists: fakeExists([]) }), undefined);
// The flags reach servers that take them; older ones get the environment.
assert.equal(toolchain.supportsTSFlags('vuka v2.5.1 (vuka, ui, web v2.5.1)'), true);
assert.equal(toolchain.supportsTSFlags('vuka v2.6.0 (vuka, ui, web v2.6.0)'), true);
assert.equal(toolchain.supportsTSFlags('vuka v2.5.0 (vuka, ui, web v2.5.0)'), false);
assert.equal(toolchain.supportsTSFlags('vuka (devel)'), false);
// The VSIX ships the server: a dependency, and its source map left out.
const pkg = JSON.parse(readFileSync(path.join(ext, 'package.json'), 'utf8'));
assert.ok(pkg.dependencies['typescript-language-server'], 'typescript-language-server is a dependency');
assert.ok(!pkg.dependencies.typescript, 'TypeScript comes from VS Code, not the VSIX');
assert.match(readFileSync(path.join(ext, '.vscodeignore'), 'utf8'), /typescript-language-server\/lib\/\*\.map/);
assert.ok(existsSync(bundled), 'npm ci installs the bundled server');
console.log('typescript: launcher ok');

const vuka = process.env.VUKA_BIN && path.resolve(process.env.VUKA_BIN);
if (!vuka) {
  console.log('typescript: no $VUKA_BIN, vuka lsp not run');
  process.exit(0);
}

// vuka lsp as the extension starts it: no typescript-language-server on
// PATH (npm test puts node_modules/.bin there), this Node as VS Code's,
// the dev dependency's TypeScript as VS Code's.
const sep = process.platform === 'win32' ? ';' : ':';
const PATH = (process.env.PATH || process.env.Path || '').split(sep).filter((d) => !['typescript-language-server', 'typescript-language-server.cmd'].some((n) => existsSync(path.join(d, n)))).join(sep);
const env = { ...process.env, PATH, Path: PATH, VUKA_TSLS: '', GOWORK: 'off', GOFLAGS: '-mod=mod' };
const lib = path.dirname(require.resolve('typescript/lib/tsserver.js'));
const launch = typescript.launcher({ extensionPath: ext, execPath: process.execPath, exists: existsSync });
launch.tsserver = lib;
const dir = mkdtempSync(path.join(tmpdir(), 'vuka-ts-'));
cpSync(path.join(here, 'fixtures-ts'), dir, { recursive: true });
const child = spawn(vuka, ['lsp', '-shared=false', ...typescript.lspArgs(launch)], { cwd: dir, env, stdio: ['pipe', 'pipe', 'inherit'] });

let buf = Buffer.alloc(0);
let id = 0;
const pending = new Map();
child.stdout.on('data', (chunk) => {
  buf = Buffer.concat([buf, chunk]);
  for (;;) {
    const end = buf.indexOf('\r\n\r\n');
    if (end < 0) return;
    const len = Number(/Content-Length: (\d+)/i.exec(buf.subarray(0, end).toString())[1]);
    if (buf.length < end + 4 + len) return;
    const msg = JSON.parse(buf.subarray(end + 4, end + 4 + len).toString());
    buf = buf.subarray(end + 4 + len);
    if (msg.id !== undefined && msg.method) {
      send({ jsonrpc: '2.0', id: msg.id, result: msg.method === 'workspace/configuration' ? (msg.params.items || []).map(() => null) : null });
    } else if (msg.id !== undefined && pending.has(msg.id)) {
      pending.get(msg.id)(msg);
      pending.delete(msg.id);
    }
  }
});
function send(msg) {
  const body = Buffer.from(JSON.stringify(msg));
  child.stdin.write(`Content-Length: ${body.length}\r\n\r\n`);
  child.stdin.write(body);
}
const call = (method, params) => new Promise((resolve, reject) => {
  const n = ++id;
  const timer = setTimeout(() => reject(new Error(`${method}: no answer`)), 60000);
  pending.set(n, (m) => { clearTimeout(timer); m.error ? reject(new Error(m.error.message)) : resolve(m.result); });
  send({ jsonrpc: '2.0', id: n, method, params });
});
const notify = (method, params) => send({ jsonrpc: '2.0', method, params });

try {
  const root = pathToFileURL(dir).href;
  await call('initialize', { processId: process.pid, rootUri: root, workspaceFolders: [{ uri: root, name: 'scripts' }],
    capabilities: { textDocument: { completion: { completionItem: { snippetSupport: true } }, hover: { contentFormat: ['markdown'] } } } });
  notify('initialized', {});
  const complete = async (file, needle, delta) => {
    const text = readFileSync(path.join(dir, file), 'utf8');
    const uri = pathToFileURL(path.join(dir, file)).href;
    notify('textDocument/didOpen', { textDocument: { uri, languageId: 'vuka-html', version: 1, text } });
    const at = text.indexOf(needle) + delta;
    const before = text.slice(0, at).split('\n');
    const position = { line: before.length - 1, character: before[before.length - 1].length };
    const res = await call('textDocument/completion', { textDocument: { uri }, position });
    return (Array.isArray(res) ? res : (res && res.items) || []).map((i) => i.label);
  };
  const live = await complete('board.vuka.html', 'live.reorder', 5);
  assert.ok(live.includes('reorder'), `live. in a page script: ${live}`);
  const ctx = await complete('chart.vuka.html', 'ctx.navigate', 4);
  for (const want of ['live', 'state', 'ref', 'on', 'emit', 'navigate', 'patch']) {
    assert.ok(ctx.includes(want), `ctx. in a component script lacks ${want}: ${ctx}`);
  }
  console.log(`typescript: vuka lsp completes live. (${live.length} items) and ctx. (${ctx.length} items) with the bundled server`);
} finally {
  child.kill();
  await new Promise((r) => child.once('exit', r));
  try { rmSync(dir, { recursive: true, force: true }); } catch (_) {}
}
