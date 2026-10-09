const vscode = require('vscode');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');
const { LanguageClient } = require('vscode-languageclient/node');

let client;

// vukaPath is the vuka binary, as an absolute path when it can be found:
// VS Code started from the Dock has a short PATH, without ~/go/bin.
function vukaPath() {
  const configured = vscode.workspace.getConfiguration('vuka').get('path') || 'vuka';
  if (path.isAbsolute(configured)) {
    return configured;
  }
  try {
    const found = execFileSync(process.env.SHELL || '/bin/sh', ['-lc', `command -v ${configured}`], { encoding: 'utf8' }).trim();
    if (found) {
      return found;
    }
  } catch (_) {}
  const candidates = [process.env.GOBIN, path.join(process.env.GOPATH || path.join(os.homedir(), 'go'), 'bin')];
  for (const dir of candidates) {
    if (dir && fs.existsSync(path.join(dir, configured))) {
      return path.join(dir, configured);
    }
  }
  return configured;
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
// drop-in gopls that also knows .vuka files.
function goplsLink(context) {
  const dir = path.join(context.globalStorageUri.fsPath, 'bin');
  const link = path.join(dir, 'gopls');
  const target = vukaPath();
  if (!path.isAbsolute(target)) {
    return undefined;
  }
  fs.mkdirSync(dir, { recursive: true });
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
  );
  if (vscode.workspace.getConfiguration('vuka').get('offerGoDropIn')) {
    enableGoDropIn(context, true).catch(() => {});
  }
  return start();
}

function deactivate() {
  return client ? client.stop() : undefined;
}

module.exports = { activate, deactivate };
