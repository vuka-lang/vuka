//! Zed extension for the Vuka language.
//!
//! Vuka is a Go superset (`.vuka` files: Go plus Result/Option, the `?`
//! operator, `match`/`case`, `@decorator` and typed `@T{…}` attributes, statics,
//! `enum` declarations, and JSX component markup). Its language server, `vuka
//! lsp`, is gopls behind a proxy: it keeps the Go generated from each `.vuka`
//! file open in gopls as an unsaved buffer and maps every request/response
//! both ways, so completion, hover, go-to-definition, references, rename,
//! outline, diagnostics and formatting all work on `.vuka` files.
//!
//! This extension launches that server. There is no bundled binary; the user
//! installs it with `go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest`.
//! The `vuka` binary is resolved, in order:
//!   1. an explicit path from Zed LSP settings (`lsp.vuka.binary.path`), else
//!   2. `vuka` on the worktree PATH.
//! Arguments default to `["lsp"]` and can be overridden via
//! `lsp.vuka.binary.arguments`.

use zed_extension_api::{
    self as zed, settings::LspSettings, Command, LanguageServerId, Result, Worktree,
};

struct VukaExtension;

impl zed::Extension for VukaExtension {
    fn new() -> Self {
        VukaExtension
    }

    fn language_server_command(
        &mut self,
        language_server_id: &LanguageServerId,
        worktree: &Worktree,
    ) -> Result<Command> {
        // Honor an explicit binary path/args from Zed settings, e.g.
        //   "lsp": { "vuka": { "binary": { "path": "/path/to/vuka",
        //                                   "arguments": ["lsp", "--log", "…"] } } }
        let settings = LspSettings::for_worktree(language_server_id.as_ref(), worktree).ok();
        let binary = settings.and_then(|s| s.binary);

        let (configured_path, configured_args) = match binary {
            Some(b) => (b.path, b.arguments),
            None => (None, None),
        };

        // Resolve the binary: configured path wins, else `vuka` on PATH.
        let command = match configured_path {
            Some(path) => path,
            None => worktree.which("vuka").ok_or_else(|| {
                "`vuka` not found on PATH. Install it with: \
                 go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest \
                 (and ensure \"$(go env GOPATH)/bin\" is on PATH), or set \
                 lsp.vuka.binary.path in your Zed settings."
                    .to_string()
            })?,
        };

        // Default to `vuka lsp` over stdio; let settings override the args.
        let args = configured_args.unwrap_or_else(|| vec!["lsp".to_string()]);

        Ok(Command {
            command,
            args,
            env: worktree.shell_env(),
        })
    }
}

zed::register_extension!(VukaExtension);
