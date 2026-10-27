// Organize imports on save: the language server's source.organizeImports
// code action, which VS Code runs before formatting when a .vuka or
// .vuka.html file is saved (the editor.codeActionsOnSave default the
// extension contributes). vuka.organizeImportsOnSave turns the save's off;
// the command and the source action menu still organize. A save never waits
// long for it: an answer later than organizeWait is dropped and the file
// saved as it is.

const organizeKind = 'source.organizeImports';

// organizeWait is how long a save waits for the organized imports, in
// milliseconds.
const organizeWait = 1500;

// automatic is CodeActionTriggerKind.Automatic: a request VS Code makes on
// its own, as on save, not one the user asked for.
const automatic = 2;

// asksOrganize reports whether a code action request's context asks for
// organize imports alone, as a save does.
function asksOrganize(context) {
  const only = context && context.only;
  const value = only && (typeof only === 'string' ? only : only.value);
  return value === organizeKind;
}

// organizeMiddleware is the client's provideCodeActions middleware: with
// enabled() false a save's request gets no action; any organize imports
// request gets its answer, or none when it comes after wait ms.
function organizeMiddleware(enabled, wait = organizeWait) {
  return (document, range, context, token, next) => {
    if (!asksOrganize(context)) {
      return next(document, range, context, token);
    }
    if (context.triggerKind === automatic && !enabled()) {
      return [];
    }
    let timer;
    const late = new Promise((resolve) => {
      timer = setTimeout(() => resolve([]), wait);
    });
    return Promise.race([Promise.resolve(next(document, range, context, token)), late]).finally(() => clearTimeout(timer));
  };
}

module.exports = { organizeKind, organizeWait, asksOrganize, organizeMiddleware };
