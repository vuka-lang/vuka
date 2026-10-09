const vscode = require('vscode');
const { LanguageClient } = require('vscode-languageclient/node');

let client;

function start() {
  const config = vscode.workspace.getConfiguration('vuka');
  const args = ['lsp'];
  const gopls = config.get('goplsPath');
  if (gopls) {
    args.push('-gopls', gopls);
  }
  client = new LanguageClient(
    'vuka',
    'Vuka',
    { command: config.get('path') || 'vuka', args },
    {
      documentSelector: [{ scheme: 'file', language: 'vuka' }],
      synchronize: {
        fileEvents: vscode.workspace.createFileSystemWatcher('**/*.{vuka,go,mod}'),
      },
    },
  );
  return client.start().catch((err) => {
    vscode.window.showErrorMessage(
      `Vuka: couldn't start "${config.get('path') || 'vuka'} lsp" (${err.message}). ` +
        'Install it with: go install github.com/vuka-lang/vuka/cmd/vuka@latest',
    );
  });
}

function activate(context) {
  context.subscriptions.push(
    vscode.commands.registerCommand('vuka.restartServer', async () => {
      if (client) {
        await client.stop();
      }
      await start();
    }),
  );
  return start();
}

function deactivate() {
  return client ? client.stop() : undefined;
}

module.exports = { activate, deactivate };
