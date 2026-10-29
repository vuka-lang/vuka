import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const { createResolver } = require('../resolve');

// A fake exec: the login shell finds vuka in /opt/bin, `vuka version` says
// v2.12.0; every run is counted.
function fake() {
  const runs = [];
  const exec = async (file, args) => {
    runs.push([file, ...args].join(' '));
    await new Promise((r) => setTimeout(r, 5));
    if (file === '/bin/zsh') {
      if (args[3] === 'vuka') return '/opt/bin/vuka\n';
      throw new Error('not found');
    }
    if (args[0] === 'version') {
      if (file === '/opt/bin/vuka') return 'vuka v2.12.0\n';
      throw new Error('no such file');
    }
    throw new Error('unexpected ' + file);
  };
  const tools = createResolver({ exec, exists: () => false, env: { SHELL: '/bin/zsh' }, platform: 'darwin', homedir: '/home/u' });
  return { tools, runs };
}

{
  // Activation asks for the path from several places at once and for the
  // version twice (the server's flags, the status item): one shell, one
  // `vuka version`.
  const { tools, runs } = fake();
  const paths = await Promise.all([tools.command(undefined, 'vuka'), tools.command('', 'vuka'), tools.command('vuka', 'vuka')]);
  assert.deepEqual(paths, ['/opt/bin/vuka', '/opt/bin/vuka', '/opt/bin/vuka']);
  const versions = await Promise.all([tools.version(paths[0]), tools.version(paths[1])]);
  assert.deepEqual(versions, ['vuka v2.12.0\n', 'vuka v2.12.0\n']);
  assert.deepEqual(runs, ['/bin/zsh -lc command -v -- "$1" sh vuka', '/opt/bin/vuka version']);

  // An absolute vuka.path is used as it is, its version read once.
  assert.equal(await tools.command('/usr/local/bin/vuka', 'vuka'), '/usr/local/bin/vuka');
  assert.equal(await tools.version('/usr/local/bin/vuka'), '');
  assert.equal(await tools.version('/usr/local/bin/vuka'), '');
  assert.equal(runs.length, 3);

  // A tool nowhere is undefined, and the setting's name is the command.
  assert.equal(await tools.findTool('templ'), undefined);
  assert.equal(await tools.command('vuka-dev', 'vuka'), 'vuka-dev');

  // Reset (vuka.path changed, or a restart): looked up again.
  runs.length = 0;
  tools.reset();
  assert.equal(await tools.command(undefined, 'vuka'), '/opt/bin/vuka');
  assert.equal(await tools.version('/opt/bin/vuka'), 'vuka v2.12.0\n');
  assert.equal(runs.length, 2);
}

{
  // Off the shell's PATH: $GOBIN, $GOPATH/bin, ~/go/bin.
  const tools = createResolver({
    exec: async () => {
      throw new Error('no shell');
    },
    exists: (p) => p === '/home/u/go/bin/vuka',
    env: {},
    platform: 'linux',
    homedir: '/home/u',
  });
  assert.equal(await tools.command(undefined, 'vuka'), '/home/u/go/bin/vuka');
}

// The extension runs nothing synchronously.
const src = (await import('node:fs')).readFileSync(new URL('../extension.js', import.meta.url), 'utf8');
assert.ok(!/execFileSync|spawnSync/.test(src), 'extension.js runs a command synchronously');
console.log('resolve tests passed');
