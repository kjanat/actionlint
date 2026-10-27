use actionlint_app::Application;
use actionlint_engine::Failure;
use actionlint_model::analysis::{Completion, Severity};
use clap::{Args, CommandFactory, Parser, Subcommand};
use std::{
    io::{self, IsTerminal, Write},
    path::PathBuf,
    process::ExitCode,
};

mod render;

#[derive(Parser)]
#[command(name = "actionlint", version, about = "GitHub Actions analysis")]
#[command(after_help = "Exit codes: 0 clean, 1 findings, 2 failed/incomplete, 3 no input")]
struct Cli {
    #[command(subcommand)]
    command: Option<Command>,
}

#[derive(Args)]
struct Selection {
    /// JSON configuration file. Plugin paths resolve relative to this file.
    #[arg(long, value_name = "FILE")]
    config: Option<PathBuf>,
    /// Analyzer component. Repeat to add components to the configured selection.
    #[arg(long = "plugin", value_name = "COMPONENT")]
    plugins: Vec<PathBuf>,
}

impl Selection {
    fn load(self) -> Result<Application, String> {
        let mut plugins = match self.config {
            Some(path) => actionlint_app::config::Config::load(&path)?.plugins,
            None => Vec::new(),
        };
        plugins.extend(self.plugins);
        Application::load(&plugins)
    }
}

#[derive(Subcommand)]
enum Command {
    /// Analyze workflow files.
    Check {
        #[command(flatten)]
        selection: Selection,
        #[arg(value_name = "WORKFLOW")]
        files: Vec<PathBuf>,
    },
    /// Serve the language server over stdio.
    Lsp {
        #[command(flatten)]
        selection: Selection,
    },
}

fn execute(
    command: Option<Command>,
    out: &mut impl Write,
    err: &mut impl Write,
    color: bool,
) -> io::Result<u8> {
    let (selection, files) = match command {
        None => {
            Cli::command().write_help(out)?;
            writeln!(out)?;
            return Ok(0);
        }
        Some(Command::Check { files, .. }) if files.is_empty() => {
            writeln!(err, "error: no workflow files to analyze")?;
            return Ok(3);
        }
        Some(Command::Lsp { selection }) => {
            let application = selection.load().map_err(io::Error::other)?;
            actionlint_lsp::stdio(application).map_err(io::Error::other)?;
            return Ok(0);
        }
        Some(Command::Check { selection, files }) => (selection, files),
    };
    let app = match selection.load() {
        Ok(app) => app,
        Err(message) => {
            writeln!(err, "error: {message}")?;
            return Ok(2);
        }
    };
    let mut exit = 0;
    for path in files {
        match app.check(&path) {
            Err(message) => {
                writeln!(err, "error: {message}")?;
                exit = 2;
            }
            Ok(run) => {
                for diagnostic in &run.analysis.diagnostics {
                    render::diagnostic(err, diagnostic, color)?;
                    if matches!(diagnostic.severity, Severity::Error | Severity::Warning) {
                        exit = exit.max(1);
                    }
                }
                for failure in run.failures {
                    let message = match failure {
                        Failure::Parse(message) => message,
                        Failure::Analyzer { name, message } => format!("{name}: {message}"),
                    };
                    writeln!(err, "error: {}: {message}", path.display())?;
                    exit = 2;
                }
                if let Completion::Incomplete(reasons) = run.analysis.completion {
                    writeln!(
                        err,
                        "warning: {}: analysis incomplete ({reasons:?})",
                        path.display()
                    )?;
                    exit = 2;
                }
            }
        }
    }
    Ok(exit)
}

fn main() -> ExitCode {
    let mut out = io::stdout();
    let mut err = io::stderr();
    let color = err.is_terminal() && std::env::var_os("NO_COLOR").is_none();
    let result = match Cli::try_parse() {
        Ok(cli) => execute(cli.command, &mut out, &mut err, color),
        Err(error) => {
            let code = if error.use_stderr() { 2 } else { 0 };
            error.print().map(|()| code)
        }
    };
    match result {
        Ok(code) => ExitCode::from(code),
        Err(error) if error.kind() == io::ErrorKind::BrokenPipe => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("error: {error}");
            ExitCode::from(2)
        }
    }
}
