// Extract component: the language server's code action runs the command
// vuka.extractComponent with the selection; the extension asks the new
// component's name, then hands the command to the server, which has the
// editor apply the edit.

const extractCommand = 'vuka.extractComponent';

// nameProblem is why name can't name a component, undefined when it can:
// a Go identifier, its first letter made upper case by the server.
function nameProblem(name) {
  if (!/^[\p{L}_][\p{L}\p{N}_]*$/u.test(name || '')) {
    return 'a Go name, such as Card or OrderRow';
  }
  return undefined;
}

// extractArguments are the command's arguments with the name added.
function extractArguments(arg, name) {
  return [{ ...arg, name }];
}

module.exports = { extractCommand, nameProblem, extractArguments };
