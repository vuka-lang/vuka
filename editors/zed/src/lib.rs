//! Zed extension for Vuka: `.vuka` and `.vuka.html` files, served by `vuka lsp`.
//!
//! The extension bundles no `vuka`. It runs the one `lsp.vuka.binary.path`
//! names, else `go tool vuka` when the worktree's go.mod declares it as a
//! tool, else the first `vuka` found on the worktree's PATH, in `$GOBIN`, in
//! each `$GOPATH/bin` or in `~/go/bin`. `<script lang="ts">` blocks get the
//! TypeScript server the extension installs with Zed's Node, unless one is on
//! PATH or `VUKA_TSLS` names one.

use std::{env, path::PathBuf};

use zed_extension_api::{
    self as zed, process, settings::LspSettings, Command, LanguageServerId,
    LanguageServerInstallationStatus, Result, Worktree,
};

const INSTALL: &str = "go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest";

/// The npm packages for script blocks: the server, and the TypeScript it
/// uses when a module has none of its own.
const TS_SERVER: (&str, &str) = ("typescript-language-server", "5.0.1");
const TYPESCRIPT: (&str, &str) = ("typescript", "6.0.3");

struct Vuka {
    typescript: Option<Vec<(String, String)>>,
}

impl zed::Extension for Vuka {
    fn new() -> Self {
        Vuka { typescript: None }
    }

    fn language_server_command(
        &mut self,
        id: &LanguageServerId,
        worktree: &Worktree,
    ) -> Result<Command> {
        let settings = LspSettings::for_worktree(id.as_ref(), worktree).unwrap_or_default();
        let binary = settings.binary;
        let mut env = worktree.shell_env();
        if let Some(extra) = binary.as_ref().and_then(|b| b.env.clone()) {
            env.extend(extra);
        }
        let configured = binary.as_ref().and_then(|b| b.path.clone());
        let mut args = binary
            .and_then(|b| b.arguments)
            .unwrap_or_else(|| lsp_args(settings.settings.as_ref()));
        let command = match configured {
            Some(path) => path,
            None => match worktree.which("go").filter(|_| declares_tool(worktree)) {
                Some(go) => {
                    args.splice(0..0, ["tool".to_string(), "vuka".to_string()]);
                    go
                }
                None => find_vuka(worktree, &env)?,
            },
        };
        if !env.iter().any(|(k, _)| k == "VUKA_TSLS") && worktree.which(TS_SERVER.0).is_none() {
            env.extend(self.typescript(id));
        }
        Ok(Command { command, args, env })
    }
}

impl Vuka {
    /// typescript is the environment naming the installed TypeScript server
    /// for vuka lsp: installed once, empty when it can't be.
    fn typescript(&mut self, id: &LanguageServerId) -> Vec<(String, String)> {
        if self.typescript.is_none() {
            self.typescript = Some(install_typescript(id).unwrap_or_default());
        }
        self.typescript.clone().unwrap_or_default()
    }
}

fn install_typescript(id: &LanguageServerId) -> Result<Vec<(String, String)>> {
    for (name, version) in [TS_SERVER, TYPESCRIPT] {
        if zed::npm_package_installed_version(name)?.as_deref() != Some(version) {
            zed::set_language_server_installation_status(
                id,
                &LanguageServerInstallationStatus::Downloading,
            );
            let installed = zed::npm_install_package(name, version);
            zed::set_language_server_installation_status(
                id,
                &LanguageServerInstallationStatus::None,
            );
            installed?;
        }
    }
    let modules = env::current_dir()
        .map_err(|e| e.to_string())?
        .join("node_modules");
    let path = |p: PathBuf| p.to_string_lossy().into_owned();
    Ok(vec![
        (
            "VUKA_TSLS".into(),
            path(modules.join(TS_SERVER.0).join("lib").join("cli.mjs")),
        ),
        ("VUKA_TSLS_NODE".into(), zed::node_binary_path()?),
        (
            "VUKA_TSSERVER".into(),
            path(modules.join(TYPESCRIPT.0).join("lib")),
        ),
    ])
}

/// declares_tool reports whether the worktree's go.mod declares vuka as a
/// tool (go get -tool), on a tool line or in a tool block.
fn declares_tool(worktree: &Worktree) -> bool {
    let Ok(module) = worktree.read_text_file("go.mod") else {
        return false;
    };
    module.lines().any(|line| {
        let line = line.trim();
        let path = line
            .strip_prefix("tool")
            .map(str::trim_start)
            .unwrap_or(line);
        path == "github.com/vuka-lang/vuka/cmd/vuka"
            || path == "github.com/vuka-lang/vuka/cmd/vuka/v2"
    })
}

/// find_vuka is the vuka binary: on PATH, else the first of the Go install
/// directories holding one that runs.
fn find_vuka(worktree: &Worktree, env: &[(String, String)]) -> Result<String> {
    if let Some(path) = worktree.which("vuka") {
        return Ok(path);
    }
    let var = |name: &str| {
        env.iter()
            .find(|(k, _)| k == name)
            .map(|(_, v)| v.clone())
            .filter(|v| !v.is_empty())
    };
    let windows = matches!(zed::current_platform().0, zed::Os::Windows);
    let home = var(if windows { "USERPROFILE" } else { "HOME" });
    let mut dirs: Vec<String> = var("GOBIN").into_iter().collect();
    if let Some(gopath) = var("GOPATH") {
        let sep = if windows { ';' } else { ':' };
        dirs.extend(gopath.split(sep).map(|p| format!("{p}/bin")));
    }
    dirs.extend(home.iter().map(|h| format!("{h}/go/bin")));
    dirs.dedup();
    let exe = if windows { "vuka.exe" } else { "vuka" };
    for dir in &dirs {
        let candidate = format!("{dir}/{exe}");
        let ran = process::Command::new(&candidate)
            .arg("version")
            .envs(env.iter().cloned())
            .output();
        if matches!(ran, Ok(out) if out.status == Some(0)) {
            return Ok(candidate);
        }
    }
    Err(format!(
        "vuka not found: not on PATH, nor in {}. Install it with `{INSTALL}`, add it to the \
         module with `go get -tool github.com/vuka-lang/vuka/cmd/vuka/v2`, or set \
         lsp.vuka.binary.path in Zed's settings, then restart the language server.",
        if dirs.is_empty() {
            "$GOBIN or ~/go/bin".into()
        } else {
            dirs.join(", ")
        },
    ))
}

/// lsp_args are vuka lsp's arguments for the settings in lsp.vuka.settings:
/// goplsPath and sharedGopls, as the VS Code extension has them.
fn lsp_args(settings: Option<&zed::serde_json::Value>) -> Vec<String> {
    let mut args = vec!["lsp".to_string()];
    let Some(settings) = settings else {
        return args;
    };
    if let Some(gopls) = settings.get("goplsPath").and_then(|v| v.as_str()) {
        if !gopls.is_empty() {
            args.extend(["-gopls".to_string(), gopls.to_string()]);
        }
    }
    if settings.get("sharedGopls").and_then(|v| v.as_bool()) == Some(false) {
        args.push("-shared=false".to_string());
    }
    args
}

zed::register_extension!(Vuka);
