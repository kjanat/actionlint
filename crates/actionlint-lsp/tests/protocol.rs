use actionlint_app::Application;
use lsp_server::{Connection, Message, Notification, Request};
use serde_json::json;

fn receive(connection: &Connection) -> Message {
    connection
        .receiver
        .recv_timeout(std::time::Duration::from_secs(30))
        .expect("LSP response timed out")
}

fn notify(connection: &Connection, method: &str, params: serde_json::Value) {
    connection
        .sender
        .send(Message::Notification(Notification::new(
            method.into(),
            params,
        )))
        .unwrap();
}

fn diagnostics(connection: &Connection) -> lsp_types::PublishDiagnosticsParams {
    let Message::Notification(notification) = receive(connection) else {
        panic!("expected diagnostics");
    };
    assert_eq!(notification.method, "textDocument/publishDiagnostics");
    serde_json::from_value(notification.params).unwrap()
}

#[test]
fn initialize_unsaved_changes_close_and_shutdown_use_real_component() {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let application =
        Application::load(&[root.join("../../target/components/workflow-rules.wasm")]).unwrap();
    let (client, server) = Connection::memory();
    let worker = std::thread::spawn(move || actionlint_lsp::serve(&server, &application));
    client
        .sender
        .send(Message::Request(Request::new(
            1.into(),
            "initialize".into(),
            json!({"capabilities": {}}),
        )))
        .unwrap();
    let Message::Response(response) = receive(&client) else {
        panic!("expected initialize response");
    };
    let result: lsp_types::InitializeResult =
        serde_json::from_value(response.response_result.unwrap()).unwrap();
    assert_eq!(
        result.capabilities.position_encoding,
        Some(lsp_types::PositionEncodingKind::UTF16)
    );
    assert!(result.capabilities.hover_provider.is_none());
    assert!(result.capabilities.code_action_provider.is_none());
    notify(&client, "initialized", json!({}));
    let uri = "file:///unsaved/workflow.yml";
    notify(
        &client,
        "textDocument/didOpen",
        json!({"textDocument": {
            "uri": uri, "languageId": "yaml", "version": 1,
            "text": "# 🦀\r\njobs:\r\n  build:\r\n    needs: missing\r\n"
        }}),
    );
    let published = diagnostics(&client);
    assert_eq!(published.version, Some(1));
    assert_eq!(published.diagnostics.len(), 1);
    assert_eq!(
        published.diagnostics[0].range.start,
        lsp_types::Position::new(3, 11)
    );
    assert_eq!(
        published.diagnostics[0].range.end,
        lsp_types::Position::new(3, 18)
    );
    notify(
        &client,
        "textDocument/didChange",
        json!({"textDocument": {"uri": uri, "version": 2}, "contentChanges": [{
            "range": {"start": {"line": 3, "character": 11}, "end": {"line": 3, "character": 18}}, "text": "[]"
        }]}),
    );
    let published = diagnostics(&client);
    assert_eq!(published.version, Some(2));
    assert!(published.diagnostics.is_empty());
    notify(
        &client,
        "textDocument/didChange",
        json!({"textDocument": {"uri": uri, "version": 2}, "contentChanges": [{"text": "bad"}]}),
    );
    let Message::Notification(error) = receive(&client) else {
        panic!("expected rejected stale edit");
    };
    assert_eq!(error.method, "window/showMessage");
    assert!(
        error.params["message"]
            .as_str()
            .unwrap()
            .contains("version")
    );
    notify(
        &client,
        "textDocument/didClose",
        json!({"textDocument": {"uri": uri}}),
    );
    assert!(diagnostics(&client).diagnostics.is_empty());
    client
        .sender
        .send(Message::Request(Request::new(
            2.into(),
            "shutdown".into(),
            (),
        )))
        .unwrap();
    let Message::Response(response) = receive(&client) else {
        panic!("expected shutdown response");
    };
    assert!(response.response_result.is_ok());
    notify(&client, "exit", json!(null));
    worker.join().unwrap().unwrap();
}

#[test]
fn independent_plugin_fix_round_trips_through_code_action_and_reanalysis() {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let application =
        Application::load(&[root.join("../../target/components/c-rule.wasm")]).unwrap();
    let (client, server) = Connection::memory();
    let worker = std::thread::spawn(move || actionlint_lsp::serve(&server, &application));
    client
        .sender
        .send(Message::Request(Request::new(
            1.into(),
            "initialize".into(),
            json!({"capabilities": {
                "workspace": {"workspaceEdit": {"documentChanges": true}},
                "textDocument": {"codeAction": {"codeActionLiteralSupport": {
                    "codeActionKind": {"valueSet": ["quickfix"]}
                }}}
            }}),
        )))
        .unwrap();
    let Message::Response(initialized) = receive(&client) else {
        panic!("expected initialize response")
    };
    let initialized: lsp_types::InitializeResult =
        serde_json::from_value(initialized.response_result.unwrap()).unwrap();
    assert!(initialized.capabilities.code_action_provider.is_some());
    notify(&client, "initialized", json!({}));
    let uri = "file:///unsaved/fix.yml";
    let text = "# 🦀\r\njobs:\r\n  build:\r\n    needs: build # comment\r\n";
    notify(
        &client,
        "textDocument/didOpen",
        json!({"textDocument": {"uri": uri, "version": 4, "languageId": "yaml", "text": text}}),
    );
    let report = diagnostics(&client);
    assert_eq!(report.diagnostics.len(), 1);
    client
        .sender
        .send(Message::Request(Request::new(
            2.into(),
            "textDocument/codeAction".into(),
            json!({
                "textDocument": {"uri": uri}, "range": report.diagnostics[0].range,
                "context": {"diagnostics": report.diagnostics}
            }),
        )))
        .unwrap();
    let Message::Response(response) = receive(&client) else {
        panic!("expected code actions");
    };
    let actions: Vec<lsp_types::CodeAction> =
        serde_json::from_value(response.response_result.unwrap()).unwrap();
    assert_eq!(actions.len(), 1);
    let Some(lsp_types::DocumentChanges::Edits(documents)) =
        &actions[0].edit.as_ref().unwrap().document_changes
    else {
        panic!("expected document edits");
    };
    assert_eq!(documents[0].text_document.version, Some(4));
    let lsp_types::OneOf::Left(edit) = &documents[0].edits[0] else {
        panic!("expected text edit");
    };
    use actionlint_model::source::{Edit, Encoding, Position, Snapshot};
    let source = Snapshot::new(uri.into(), text.into());
    let start = source
        .offset(
            Position {
                line: usize::try_from(edit.range.start.line).unwrap(),
                character: usize::try_from(edit.range.start.character).unwrap(),
            },
            Encoding::Utf16,
        )
        .unwrap();
    let end = source
        .offset(
            Position {
                line: usize::try_from(edit.range.end.line).unwrap(),
                character: usize::try_from(edit.range.end.character).unwrap(),
            },
            Encoding::Utf16,
        )
        .unwrap();
    let changed = source
        .apply(&[Edit {
            span: source.span(start..end).unwrap(),
            replacement: edit.new_text.clone(),
        }])
        .unwrap();
    assert!(changed.text().contains("needs: [] # comment\r\n"));
    notify(
        &client,
        "textDocument/didChange",
        json!({"textDocument": {"uri": uri, "version": 5}, "contentChanges": [{"text": changed.text()}]}),
    );
    assert!(diagnostics(&client).diagnostics.is_empty());
    client
        .sender
        .send(Message::Request(Request::new(
            3.into(),
            "shutdown".into(),
            (),
        )))
        .unwrap();
    assert!(matches!(receive(&client), Message::Response(_)));
    notify(&client, "exit", json!(null));
    worker.join().unwrap().unwrap();
}

#[test]
fn clients_without_versioned_literal_edits_receive_diagnostics_but_no_fixes() {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let application =
        Application::load(&[root.join("../../target/components/c-rule.wasm")]).unwrap();
    for capabilities in [
        json!({}),
        json!({"workspace": {"workspaceEdit": {"documentChanges": true}}}),
        json!({"textDocument": {"codeAction": {"codeActionLiteralSupport": {
            "codeActionKind": {"valueSet": ["quickfix"]}
        }}}}),
        json!({
            "workspace": {"workspaceEdit": {"documentChanges": false}},
            "textDocument": {"codeAction": {"codeActionLiteralSupport": {
                "codeActionKind": {"valueSet": ["quickfix"]}
            }}}
        }),
    ] {
        std::thread::scope(|scope| {
            let (client, server) = Connection::memory();
            let application = &application;
            let worker = scope.spawn(move || actionlint_lsp::serve(&server, application));
            client
                .sender
                .send(Message::Request(Request::new(
                    1.into(),
                    "initialize".into(),
                    json!({"capabilities": capabilities}),
                )))
                .unwrap();
            let Message::Response(response) = receive(&client) else {
                panic!("expected initialization")
            };
            let initialized: lsp_types::InitializeResult =
                serde_json::from_value(response.response_result.unwrap()).unwrap();
            assert!(initialized.capabilities.code_action_provider.is_none());
            notify(&client, "initialized", json!({}));
            let uri = "file:///workflow.yml";
            notify(
                &client,
                "textDocument/didOpen",
                json!({"textDocument": {
                    "uri": uri, "version": 1, "languageId": "yaml",
                    "text": "jobs:\n  build:\n    needs: build\n"
                }}),
            );
            let report = diagnostics(&client);
            assert_eq!(report.diagnostics.len(), 1);
            client
                .sender
                .send(Message::Request(Request::new(
                    2.into(),
                    "textDocument/codeAction".into(),
                    json!({
                        "textDocument": {"uri": uri}, "range": report.diagnostics[0].range,
                        "context": {"diagnostics": report.diagnostics}
                    }),
                )))
                .unwrap();
            let Message::Response(response) = receive(&client) else {
                panic!("expected rejected request")
            };
            assert_eq!(response.response_result.unwrap_err().code, -32601);
            client
                .sender
                .send(Message::Request(Request::new(
                    3.into(),
                    "shutdown".into(),
                    (),
                )))
                .unwrap();
            let Message::Response(response) = receive(&client) else {
                panic!("expected shutdown")
            };
            assert!(response.response_result.is_ok());
            notify(&client, "exit", json!(null));
            worker.join().unwrap().unwrap();
        });
    }
}
