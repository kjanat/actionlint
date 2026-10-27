use super::*;
use lsp_server::Request;
use serde_json::json;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::Duration;

fn receive(connection: &Connection) -> Message {
    connection
        .receiver
        .recv_timeout(Duration::from_secs(30))
        .unwrap()
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

#[test]
fn busy_analysis_does_not_block_changes_and_only_latest_version_is_published() {
    pending_changes(false);
}

#[test]
fn close_and_reopen_discards_inflight_and_queued_results() {
    pending_changes(true);
}

fn pending_changes(reopen: bool) {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"));
    let app =
        Application::load(&[root.join("../../target/components/workflow-rules.wasm")]).unwrap();
    let (client, server) = Connection::memory();
    let (started, observed) = crossbeam_channel::bounded(3);
    let (release, gate) = crossbeam_channel::bounded(1);
    let worker = std::thread::spawn(move || {
        let first = AtomicBool::new(true);
        run_messages(&server, false, move |capture| {
            started.send(capture.source().text().to_owned()).unwrap();
            if first.swap(false, Ordering::SeqCst) {
                gate.recv_timeout(Duration::from_secs(30)).unwrap();
            }
            capture.analyze(&app)
        })
    });
    let uri = "file:///unsaved.yml";
    notify(
        &client,
        "textDocument/didOpen",
        json!({"textDocument": {
            "uri": uri, "version": 1, "languageId": "yaml", "text": "jobs: {build: {needs: missing}}"
        }}),
    );
    assert!(
        observed
            .recv_timeout(Duration::from_secs(30))
            .unwrap()
            .contains("missing")
    );
    for version in [2, 3] {
        notify(
            &client,
            "textDocument/didChange",
            json!({
                "textDocument": {"uri": uri, "version": version},
                "contentChanges": [{"text": format!("# version {version}\njobs: {{build: {{needs: []}}}}") }]
            }),
        );
    }
    if reopen {
        notify(
            &client,
            "textDocument/didClose",
            json!({"textDocument": {"uri": uri}}),
        );
        let Message::Notification(notification) = receive(&client) else {
            panic!("expected clear diagnostics");
        };
        let params: lsp::PublishDiagnosticsParams =
            serde_json::from_value(notification.params).unwrap();
        assert!(params.diagnostics.is_empty());
        notify(
            &client,
            "textDocument/didOpen",
            json!({"textDocument": {
                "uri": uri, "version": 1, "languageId": "yaml", "text": "# reopened\njobs: {build: {needs: []}}"
            }}),
        );
    }
    client
        .sender
        .send(Message::Request(Request::new(
            10.into(),
            "test/barrier".into(),
            (),
        )))
        .unwrap();
    let Message::Response(response) = receive(&client) else {
        panic!("analysis blocked message processing");
    };
    assert_eq!(response.id, 10.into());
    assert_eq!(response.response_result.unwrap_err().code, -32601);
    release.send(()).unwrap();
    let text = observed.recv_timeout(Duration::from_secs(30)).unwrap();
    assert!(
        text.starts_with(if reopen { "# reopened" } else { "# version 3" }),
        "{text}"
    );
    let Message::Notification(notification) = receive(&client) else {
        panic!("expected diagnostics");
    };
    assert_eq!(notification.method, "textDocument/publishDiagnostics");
    let params: lsp::PublishDiagnosticsParams =
        serde_json::from_value(notification.params).unwrap();
    assert_eq!(params.version, Some(if reopen { 1 } else { 3 }));
    assert!(params.diagnostics.is_empty());
    client
        .sender
        .send(Message::Request(Request::new(
            11.into(),
            "shutdown".into(),
            (),
        )))
        .unwrap();
    assert!(matches!(receive(&client), Message::Response(_)));
    notify(&client, "exit", json!(null));
    worker.join().unwrap().unwrap();
    assert!(observed.try_recv().is_err());
}
