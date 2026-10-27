use std::{
    env,
    error::Error,
    ffi::OsString,
    fs,
    path::{Path, PathBuf},
    process::{Command, ExitCode},
};

type Result<T> = std::result::Result<T, Box<dyn Error>>;

fn run(command: &mut Command) -> Result<()> {
    eprintln!("{command:?}");
    let status = command.status()?;
    if !status.success() {
        return Err(format!("command failed: {status}").into());
    }
    Ok(())
}

fn tool(variable: &str, default: &str) -> OsString {
    env::var_os(variable).unwrap_or_else(|| default.into())
}

fn clang() -> OsString {
    env::var_os("WASI_SDK_PATH")
        .map(|sdk| {
            PathBuf::from(sdk)
                .join("bin")
                .join(format!("wasm32-wasip2-clang{}", env::consts::EXE_SUFFIX))
                .into_os_string()
        })
        .unwrap_or_else(|| "wasm32-wasip2-clang".into())
}

fn compile_c(source: &Path, bindings: &Path, output: &Path) -> Command {
    let mut command = Command::new(clang());
    command
        .args([
            "-std=c11",
            "-O2",
            "-Wall",
            "-Wextra",
            "-Werror",
            "-mexec-model=reactor",
            "-I",
        ])
        .arg(bindings)
        .arg(source)
        .arg(bindings.join("analyzer.c"))
        .arg(bindings.join("analyzer_component_type.o"))
        .arg("-o")
        .arg(output);
    command
}

fn generate_c(contract: &Path, output: &Path) -> Result<()> {
    run(Command::new(tool("WIT_BINDGEN", "wit-bindgen"))
        .args(["c", "--world", "analyzer", "--out-dir"])
        .arg(output)
        .arg(contract))
}

fn components() -> Result<()> {
    fs::create_dir_all("target/components")?;
    run(Command::new(tool("CARGO", "cargo")).args([
        "build",
        "--locked",
        "-p",
        "actionlint-workflow-rules",
        "--target",
        "wasm32-unknown-unknown",
        "--target-dir",
        "target",
    ]))?;
    run(Command::new(tool("WASM_TOOLS", "wasm-tools")).args([
        "component",
        "new",
        "target/wasm32-unknown-unknown/debug/actionlint_workflow_rules.wasm",
        "-o",
        "target/components/workflow-rules.wasm",
    ]))?;
    generate_c(Path::new("wit"), Path::new("target/c-rule-bindings"))?;
    run(&mut compile_c(
        Path::new("tests/guests/c-rule/self_dependency.c"),
        Path::new("target/c-rule-bindings"),
        Path::new("target/components/c-rule.wasm"),
    ))?;
    run(&mut compile_c(
        Path::new("tests/guests/c-rule/mapped_expression.c"),
        Path::new("target/c-rule-bindings"),
        Path::new("target/components/c-expression.wasm"),
    ))?;
    run(Command::new(tool("WASM_TOOLS", "wasm-tools")).args([
        "parse",
        "tests/guests/incompatible-analysis.wat",
        "-o",
        "target/components/incompatible-analysis.wasm",
    ]))?;
    for artifact in [
        "workflow-rules",
        "c-rule",
        "c-expression",
        "incompatible-analysis",
    ] {
        run(Command::new(tool("WASM_TOOLS", "wasm-tools"))
            .arg("validate")
            .arg(format!("target/components/{artifact}.wasm")))?;
    }
    run(Command::new(tool("CARGO", "cargo")).args([
        "test",
        "--locked",
        "-p",
        "actionlint-component-host",
        "--test",
        "rules_component",
        "--test",
        "c_component",
        "--test",
        "incompatible_component",
    ]))?;
    run(Command::new(tool("CARGO", "cargo")).args([
        "test",
        "--locked",
        "-p",
        "actionlint-cli",
        "-p",
        "actionlint-app",
        "-p",
        "actionlint-lsp",
        "--features",
        "actionlint-app/schema",
    ]))
}

fn negative_contract() -> Result<()> {
    let temporary_root = env::temp_dir().canonicalize()?;
    let directory = temporary_root.join(format!(
        "actionlint-contract-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)?
            .as_nanos()
    ));
    fs::create_dir(&directory)?;
    let outcome = incompatible_c_signature(&directory);
    let resolved = directory.canonicalize()?;
    if resolved.parent() != Some(temporary_root.as_path()) {
        return Err("refusing cleanup outside the temporary root".into());
    }
    let cleanup = fs::remove_dir_all(&resolved);
    outcome?;
    cleanup?;
    Ok(())
}

fn incompatible_c_signature(directory: &Path) -> Result<()> {
    let contract = directory.join("wit");
    fs::create_dir(&contract)?;
    for file in ["types.wit", "analysis.wit", "worlds.wit"] {
        fs::copy(Path::new("wit").join(file), contract.join(file))?;
    }
    let analysis = contract.join("analysis.wit");
    let original = fs::read_to_string(&analysis)?;
    let signature = "result<analysis-result, failure>";
    if original.matches(signature).count() != 1 {
        return Err("negative contract fixture cannot locate the analysis signature".into());
    }
    fs::write(
        analysis,
        original.replace(signature, "result<u32, failure>"),
    )?;
    let bindings = directory.join("bindings");
    generate_c(&contract, &bindings)?;
    let output = compile_c(
        Path::new("tests/guests/c-rule/self_dependency.c"),
        &bindings,
        &directory.join("invalid.wasm"),
    )
    .output()?;
    if output.status.success() {
        return Err("incompatible WIT signature unexpectedly compiled".into());
    }
    let errors = String::from_utf8_lossy(&output.stderr);
    if !errors.contains("conflicting types for 'exports_actionlint_extension_analysis_analyze'")
        || !errors.contains("uint32_t *ret")
    {
        return Err(format!(
            "compilation failed without the expected contract type error:\n{errors}"
        )
        .into());
    }
    eprintln!("Incompatible WIT export rejected by unchanged C implementation.");
    Ok(())
}

fn execute() -> Result<()> {
    env::set_current_dir(
        Path::new(env!("CARGO_MANIFEST_DIR"))
            .parent()
            .ok_or("workspace root missing")?,
    )?;
    match env::args().nth(1).as_deref() {
        Some("component-conformance") => {
            components()?;
            negative_contract()
        }
        Some("contract-negative") => negative_contract(),
        _ => Err("usage: cargo run -p xtask -- component-conformance | contract-negative".into()),
    }
}

fn main() -> ExitCode {
    match execute() {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("{error}");
            ExitCode::FAILURE
        }
    }
}
