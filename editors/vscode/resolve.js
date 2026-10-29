// Finding the tools the extension runs, without holding up the editor: a
// login shell finds a command on the PATH the user's shell has (VS Code
// started from the Dock has a short one, without ~/go/bin), and that takes
// a while. Each answer is kept: a command's path until the setting naming
// it changes, the output of `vuka version` until the path does.

const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFile } = require('child_process');

// run is execFile as a promise of its stdout; it rejects on an error.
function run(file, args, options) {
  return new Promise((resolve, reject) => {
    execFile(file, args, { encoding: 'utf8', ...options }, (err, stdout) => (err ? reject(err) : resolve(stdout)));
  });
}

// createResolver makes a resolver; the options replace what it runs and
// reads, for tests.
function createResolver({ exec = run, exists = fs.existsSync, env = process.env, platform = process.platform, homedir = os.homedir() } = {}) {
  const tools = new Map(); // name → Promise<absolute path | undefined>
  const versions = new Map(); // command → Promise<`vuka version`'s output, '' when it fails>

  // lookup is a command's absolute path: on the login shell's PATH, else
  // in $GOBIN, $GOPATH/bin or ~/go/bin; undefined when it's nowhere.
  async function lookup(name) {
    const win = platform === 'win32';
    try {
      const out = win
        ? (await exec('where', [name], {})).split(/\r?\n/)[0].trim()
        : (await exec(env.SHELL || '/bin/sh', ['-lc', 'command -v -- "$1"', 'sh', name], {})).trim();
      if (out && path.isAbsolute(out)) {
        return out;
      }
    } catch (_) {}
    const exe = win ? name + '.exe' : name;
    const candidates = [env.GOBIN, path.join(env.GOPATH || path.join(homedir, 'go'), 'bin'), path.join(homedir, 'go', 'bin')];
    for (const dir of candidates) {
      if (dir && exists(path.join(dir, exe))) {
        return path.join(dir, exe);
      }
    }
    return undefined;
  }

  // findTool is lookup's answer for name, looked up once.
  function findTool(name) {
    if (!tools.has(name)) {
      tools.set(name, lookup(name));
    }
    return tools.get(name);
  }

  // command is the binary the setting names (configured, default name):
  // an absolute path as it is, else where findTool finds it, else the name.
  async function command(configured, name) {
    const want = configured || name;
    if (path.isAbsolute(want)) {
      return want;
    }
    return (await findTool(want)) || want;
  }

  // version is `<command> version`'s output, '' when it can't be run.
  function version(cmd) {
    if (!versions.has(cmd)) {
      versions.set(cmd, exec(cmd, ['version'], { timeout: 5000 }).catch(() => ''));
    }
    return versions.get(cmd);
  }

  // reset forgets every answer: the tools may have been installed or
  // updated since.
  function reset() {
    tools.clear();
    versions.clear();
  }

  return { findTool, command, version, reset };
}

module.exports = { createResolver };
