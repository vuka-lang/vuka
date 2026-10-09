const vscode = require('vscode');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');
const { LanguageClient, RevealOutputChannelOn } = require('vscode-languageclient/node');

let client;

// vukaPath is the vuka binary, as an absolute path when it can be found:
// VS Code started from the Dock has a short PATH, without ~/go/bin.
function vukaPath() {
  const configured = vscode.workspace.getConfiguration('vuka').get('path') || 'vuka';
  return path.isAbsolute(configured) ? configured : findTool(configured) || configured;
}

// findTool is a command's absolute path: on the login shell's PATH, else in
// $GOBIN, $GOPATH/bin or ~/go/bin; undefined when it's nowhere.
function findTool(name) {
  const win = process.platform === 'win32';
  try {
    const found = win
      ? execFileSync('where', [name], { encoding: 'utf8' }).split(/\r?\n/)[0].trim()
      : execFileSync(process.env.SHELL || '/bin/sh', ['-lc', `command -v ${name}`], { encoding: 'utf8' }).trim();
    if (found && path.isAbsolute(found)) {
      return found;
    }
  } catch (_) {}
  const exe = win ? name + '.exe' : name;
  const candidates = [process.env.GOBIN, path.join(process.env.GOPATH || path.join(os.homedir(), 'go'), 'bin'), path.join(os.homedir(), 'go', 'bin')];
  for (const dir of candidates) {
    if (dir && fs.existsSync(path.join(dir, exe))) {
      return path.join(dir, exe);
    }
  }
  return undefined;
}

function start() {
  const config = vscode.workspace.getConfiguration('vuka');
  const args = ['lsp'];
  const gopls = config.get('goplsPath');
  if (gopls) {
    args.push('-gopls', gopls);
  }
  if (!config.get('sharedGopls')) {
    args.push('-shared=false');
  }
  const command = vukaPath();
  client = new LanguageClient(
    'vuka',
    'Vuka',
    { command, args },
    {
      documentSelector: [{ scheme: 'file', language: 'vuka' }],
      // Like the Go extension: the log is there when wanted, never in the way.
      revealOutputChannelOn: RevealOutputChannelOn.Never,
      synchronize: {
        fileEvents: vscode.workspace.createFileSystemWatcher('**/*.{vuka,go,mod}'),
      },
    },
  );
  return client.start().catch((err) => {
    vscode.window.showErrorMessage(
      `Vuka: couldn't start "${command} lsp" (${err.message}). ` +
        'Install it with: go install github.com/vuka-lang/vuka/cmd/vuka@latest',
    );
  });
}

// goplsLink is a link named gopls to vuka, which run under that name is a
// drop-in gopls that also knows .vuka files. Windows gets a hard link (or a
// copy) named gopls.exe, refreshed while no gopls runs from it.
function goplsLink(context) {
  const dir = binDir(context);
  const target = vukaPath();
  if (!path.isAbsolute(target)) {
    return undefined;
  }
  fs.mkdirSync(dir, { recursive: true });
  if (process.platform === 'win32') {
    const exe = path.join(dir, 'gopls.exe');
    try {
      fs.rmSync(exe, { force: true });
      try {
        fs.linkSync(target, exe);
      } catch (_) {
        fs.copyFileSync(target, exe);
      }
    } catch (_) {}
    return fs.existsSync(exe) ? exe : undefined;
  }
  const link = path.join(dir, 'gopls');
  try {
    if (fs.readlinkSync(link) === target) {
      return link;
    }
    fs.unlinkSync(link);
  } catch (_) {}
  fs.symlinkSync(target, link);
  return link;
}

async function enableGoDropIn(context, ask) {
  if (process.platform === 'win32' || !vscode.extensions.getExtension('golang.go')) {
    return;
  }
  const link = goplsLink(context);
  if (!link) {
    return;
  }
  const goConfig = vscode.workspace.getConfiguration('go');
  const tools = goConfig.get('alternateTools') || {};
  if (tools.gopls === link) {
    return;
  }
  if (tools.gopls && ask) {
    return; // the user chose another gopls; leave it
  }
  if (ask) {
    if (context.globalState.get('vuka.goDropInDeclined')) {
      return;
    }
    const choice = await vscode.window.showInformationMessage(
      'Vuka can serve your Go files too: the Go extension then sees code from .vuka files, and one shared gopls does the work.',
      'Enable',
      'Not now',
      "Don't ask again",
    );
    if (choice === "Don't ask again") {
      await context.globalState.update('vuka.goDropInDeclined', true);
    }
    if (choice !== 'Enable') {
      return;
    }
  }
  await goConfig.update('alternateTools', { ...tools, gopls: link }, vscode.ConfigurationTarget.Global);
  const reload = await vscode.window.showInformationMessage('Vuka now serves Go files. Reload the window to switch.', 'Reload');
  if (reload === 'Reload') {
    await vscode.commands.executeCommand('workbench.action.reloadWindow');
  }
}

async function disableGoDropIn(context) {
  const goConfig = vscode.workspace.getConfiguration('go');
  const tools = { ...(goConfig.get('alternateTools') || {}) };
  if (tools.gopls && tools.gopls.startsWith(context.globalStorageUri.fsPath)) {
    delete tools.gopls;
    await goConfig.update('alternateTools', tools, vscode.ConfigurationTarget.Global);
    vscode.window.showInformationMessage('Go files are served by plain gopls again after a reload.');
  }
}

function binDir(context) {
  return path.join(context.globalStorageUri.fsPath, 'bin');
}

// templWrapper is the templ the templ extension runs, once Vuka serves .templ
// files' Go: a script beside the gopls link that puts the link first on PATH
// and runs the real templ, whose language server then uses Vuka as its gopls.
function templWrapper(context) {
  return path.join(binDir(context), process.platform === 'win32' ? 'templ.cmd' : 'templ');
}

function writeTemplWrapper(context, templ) {
  const file = templWrapper(context);
  const dir = path.dirname(file);
  if (process.platform === 'win32') {
    fs.writeFileSync(file, `@echo off\r\nset "PATH=${dir};%PATH%"\r\n"${templ}" %*\r\n`);
  } else {
    const q = (s) => `'${s.replace(/'/g, `'\\''`)}'`;
    fs.writeFileSync(file, `#!/bin/sh\nPATH=${q(dir)}:"$PATH"\nexport PATH\nexec ${q(templ)} "$@"\n`, { mode: 0o755 });
    fs.chmodSync(file, 0o755);
  }
  return file;
}

async function hasFiles(glob) {
  return (await vscode.workspace.findFiles(glob, '**/node_modules/**', 1)).length > 0;
}

async function restartTempl(message) {
  const choice = await vscode.window.showInformationMessage(message, 'Restart templ language server');
  if (choice) {
    const commands = await vscode.commands.getCommands(true);
    await vscode.commands.executeCommand(commands.includes('templ.restartServer') ? 'templ.restartServer' : 'workbench.action.reloadWindow');
  }
}

async function enableTemplDropIn(context, ask) {
  if (!vscode.extensions.getExtension('a-h.templ')) {
    if (!ask) {
      vscode.window.showInformationMessage('Install the templ extension (a-h.templ) first.');
    }
    return;
  }
  if (ask && (context.globalState.get('vuka.templDropInDeclined') || !(await hasFiles('**/*.templ')) || !(await hasFiles('**/*.vuka')))) {
    return;
  }
  const templConfig = vscode.workspace.getConfiguration('templ');
  const current = templConfig.get('executablePath');
  const wrapper = templWrapper(context);
  if (current && current !== wrapper) {
    if (!ask) {
      vscode.window.showInformationMessage(
        `templ.executablePath is set to ${current}; Vuka leaves it alone. Run templ with ${binDir(context)} first on PATH for its gopls to be Vuka.`,
      );
    }
    return;
  }
  const templ = findTool('templ');
  const link = goplsLink(context);
  if (!templ || !link) {
    if (!ask) {
      vscode.window.showErrorMessage(templ ? 'Vuka: vuka not found (vuka.path).' : 'Vuka: templ not found; install it with: go install github.com/a-h/templ/cmd/templ@latest');
    }
    return;
  }
  if (current === wrapper && ask) {
    writeTemplWrapper(context, templ); // kept pointing at templ as it is now
    return;
  }
  if (ask) {
    const choice = await vscode.window.showInformationMessage(
      'templ files can see your Vuka code: let templ use Vuka as its gopls?',
      'Enable',
      'Not now',
      "Don't ask again",
    );
    if (choice === "Don't ask again") {
      await context.globalState.update('vuka.templDropInDeclined', true);
    }
    if (choice !== 'Enable') {
      return;
    }
  }
  await templConfig.update('executablePath', writeTemplWrapper(context, templ), vscode.ConfigurationTarget.Global);
  await restartTempl('templ now uses Vuka as its gopls: .templ files see code from .vuka files.');
}

async function disableTemplDropIn(context) {
  const templConfig = vscode.workspace.getConfiguration('templ');
  const wrapper = templWrapper(context);
  if (templConfig.get('executablePath') !== wrapper) {
    return;
  }
  await templConfig.update('executablePath', undefined, vscode.ConfigurationTarget.Global);
  fs.rmSync(wrapper, { force: true });
  await restartTempl('templ uses plain gopls again.');
}

function activate(context) {
  context.subscriptions.push(
    vscode.commands.registerCommand('vuka.restartServer', async () => {
      if (client) {
        // A client whose server failed to start can't be stopped cleanly.
        await client.stop().catch(() => {});
        client = undefined;
      }
      await start();
    }),
    vscode.commands.registerCommand('vuka.serveGoFiles', () => enableGoDropIn(context, false)),
    vscode.commands.registerCommand('vuka.stopServingGoFiles', () => disableGoDropIn(context)),
    vscode.commands.registerCommand('vuka.serveTemplFiles', () => enableTemplDropIn(context, false)),
    vscode.commands.registerCommand('vuka.stopServingTemplFiles', () => disableTemplDropIn(context)),
  );
  const config = vscode.workspace.getConfiguration('vuka');
  if (config.get('offerGoDropIn')) {
    enableGoDropIn(context, true).catch(() => {});
  }
  if (config.get('offerTemplDropIn')) {
    enableTemplDropIn(context, true).catch(() => {});
  }
  return start();
}

function deactivate() {
  return client ? client.stop() : undefined;
}

module.exports = { activate, deactivate };
