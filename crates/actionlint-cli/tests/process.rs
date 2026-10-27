use std::process::Command;

#[test]
fn config_resolves_components_relative_to_its_own_directory() {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let output = Command::new(env!("CARGO_BIN_EXE_actionlint"))
        .current_dir(root.join("../.."))
        .arg("check")
        .arg("--config")
        .arg(root.join("tests/fixtures/actionlint.json"))
        .arg(root.join("tests/fixtures/missing.yml"))
        .output()
        .unwrap();
    assert_eq!(output.status.code(), Some(1), "{:?}", output);
    assert!(
        String::from_utf8(output.stderr)
            .unwrap()
            .contains("workflow/undefined-dependency")
    );
}

#[test]
fn clap_handles_subcommand_help_version_and_missing_values() {
    for arguments in [
        vec!["check", "--help"],
        vec!["help", "lsp"],
        vec!["--version"],
    ] {
        let output = Command::new(env!("CARGO_BIN_EXE_actionlint"))
            .args(arguments)
            .output()
            .unwrap();
        assert_eq!(output.status.code(), Some(0));
        assert!(!output.stdout.is_empty());
        assert!(output.stderr.is_empty());
    }
    for arguments in [
        vec!["check", "--plugin"],
        vec!["lsp", "unexpected.yml"],
        vec!["check", "--config"],
    ] {
        let output = Command::new(env!("CARGO_BIN_EXE_actionlint"))
            .args(arguments)
            .output()
            .unwrap();
        assert_eq!(output.status.code(), Some(2));
        assert!(!output.stderr.is_empty());
    }
}

#[test]
fn no_arguments_print_help_and_succeed() {
    let output = Command::new(env!("CARGO_BIN_EXE_actionlint"))
        .output()
        .unwrap();
    assert_eq!(output.status.code(), Some(0));
    assert!(String::from_utf8(output.stdout).unwrap().contains("Usage:"));
    assert!(output.stderr.is_empty());
}

#[test]
fn explicit_check_without_input_exits_three() {
    let output = Command::new(env!("CARGO_BIN_EXE_actionlint"))
        .arg("check")
        .output()
        .unwrap();
    assert_eq!(output.status.code(), Some(3));
    assert!(
        String::from_utf8(output.stderr)
            .unwrap()
            .contains("no workflow files")
    );
}

#[test]
fn unknown_options_are_errors() {
    let output = Command::new(env!("CARGO_BIN_EXE_actionlint"))
        .args(["check", "--unknown"])
        .output()
        .unwrap();
    assert_eq!(output.status.code(), Some(2));
}

#[test]
fn component_findings_and_incomplete_analysis_have_distinct_exit_codes() {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let component = root.join("../../target/components/workflow-rules.wasm");
    assert!(
        component.is_file(),
        "build component-conformance artifacts first"
    );
    for (fixture, code, message) in [
        ("missing.yml", 1, "error[workflow/undefined-dependency]"),
        ("clean.yml", 0, ""),
        ("dynamic.yml", 2, "analysis incomplete"),
    ] {
        let output = Command::new(env!("CARGO_BIN_EXE_actionlint"))
            .arg("check")
            .arg("--plugin")
            .arg(&component)
            .arg(root.join("tests/fixtures").join(fixture))
            .output()
            .unwrap();
        let stderr = String::from_utf8(output.stderr).unwrap();
        assert_eq!(output.status.code(), Some(code), "{fixture}: {stderr}");
        if code == 0 {
            assert!(stderr.is_empty(), "{stderr}");
        } else {
            assert!(stderr.contains(message), "{stderr}");
        }
        if code == 1 {
            assert!(stderr.contains("needs: missing"), "{stderr}");
            assert!(stderr.contains("^^^^^^^"), "{stderr}");
        }
    }
}

#[test]
fn repeated_plugin_option_executes_both_components() {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let output = Command::new(env!("CARGO_BIN_EXE_actionlint"))
        .arg("check")
        .arg("--plugin")
        .arg(root.join("../../target/components/workflow-rules.wasm"))
        .arg("--plugin")
        .arg(root.join("../../target/components/c-rule.wasm"))
        .arg(root.join("tests/fixtures/self.yml"))
        .output()
        .unwrap();
    let stderr = String::from_utf8(output.stderr).unwrap();
    assert_eq!(output.status.code(), Some(1), "{stderr}");
    assert!(stderr.contains("workflow/dependency-cycle"), "{stderr}");
    assert!(stderr.contains("workflow/self-dependency"), "{stderr}");
}
