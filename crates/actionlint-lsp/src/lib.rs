//! LSP transport and projections over application-owned editor buffers.

use actionlint_app::{
    Application,
    editor::{Capture, Change, Completed, Publication, Session},
};
use actionlint_model::{
    analysis::{Completion, Severity},
    source::{Encoding, Position, Span},
};
use lsp_server::{Connection, Message, Notification, Response};
use lsp_types::{
    self as lsp,
    notification::{Notification as _, PublishDiagnostics, ShowMessage},
};

use std::collections::BTreeMap;

mod actions;
use actions::ActionCache;

type Result<T> = std::result::Result<T, Box<dyn std::error::Error + Send + Sync>>;

pub fn stdio(application: Application) -> Result<()> {
    let (connection, threads) = Connection::stdio();
    let result = serve(&connection, &application);
    drop(connection);
    threads.join()?;
    result
}

pub fn serve(connection: &Connection, application: &Application) -> Result<()> {
    let (id, params) = connection.initialize_start()?;
    let params: lsp::InitializeParams = serde_json::from_value(params)?;
    let quickfixes = supports_versioned_actions(&params.capabilities);
    let capabilities = lsp::ServerCapabilities {
        position_encoding: Some(lsp::PositionEncodingKind::UTF16),
        code_action_provider: quickfixes.then_some(lsp::CodeActionProviderCapability::Options(
            lsp::CodeActionOptions {
                code_action_kinds: Some(vec![lsp::CodeActionKind::QUICKFIX]),
                ..Default::default()
            },
        )),
        text_document_sync: Some(lsp::TextDocumentSyncCapability::Options(
            lsp::TextDocumentSyncOptions {
                open_close: Some(true),
                change: Some(lsp::TextDocumentSyncKind::INCREMENTAL),
                ..Default::default()
            },
        )),
        ..Default::default()
    };
    connection.initialize_finish(
        id,
        serde_json::to_value(lsp::InitializeResult {
            capabilities,
            server_info: Some(lsp::ServerInfo {
                name: "actionlint".into(),
                version: Some(env!("CARGO_PKG_VERSION").into()),
            }),
        })?,
    )?;
    run_messages(connection, quickfixes, |capture| {
        capture.analyze(application)
    })
}

fn supports_versioned_actions(capabilities: &lsp::ClientCapabilities) -> bool {
    let literals = capabilities
        .text_document
        .as_ref()
        .and_then(|document| document.code_action.as_ref())
        .and_then(|actions| actions.code_action_literal_support.as_ref())
        .is_some();
    let versions = capabilities
        .workspace
        .as_ref()
        .and_then(|workspace| workspace.workspace_edit.as_ref())
        .and_then(|edit| edit.document_changes)
        == Some(true);
    literals && versions
}

fn run_messages(
    connection: &Connection,
    quickfixes: bool,
    analyze: impl Fn(Capture) -> Completed + Send,
) -> Result<()> {
    std::thread::scope(|scope| {
        let (jobs, work) = crossbeam_channel::bounded::<Capture>(1);
        let (finished, results) = crossbeam_channel::bounded::<Completed>(1);
        let worker = scope.spawn(move || {
            for capture in work {
                if finished.send(analyze(capture)).is_err() {
                    break;
                }
            }
        });
        let outcome = process_messages(connection, &jobs, &results, quickfixes);
        drop(jobs);
        drop(results);
        worker.join().map_err(|_| "analysis worker panicked")?;
        outcome
    })
}

fn process_messages(
    connection: &Connection,
    jobs: &crossbeam_channel::Sender<Capture>,
    results: &crossbeam_channel::Receiver<Completed>,
    quickfixes: bool,
) -> Result<()> {
    let mut session = Session::default();
    let mut pending = BTreeMap::new();
    let mut actions = ActionCache::default();
    let mut busy = false;
    loop {
        if !busy && let Some((_, capture)) = pending.pop_first() {
            jobs.send(capture)?;
            busy = true;
        }
        crossbeam_channel::select_biased! {
            recv(connection.receiver) -> message => {
                match message.map_err(|_| "client disconnected before shutdown")? {
                    Message::Request(request) => {
                        if connection.handle_shutdown(&request)? { return Ok(()); }
                        let response = if quickfixes && request.method == "textDocument/codeAction" {
                            match serde_json::from_value::<lsp::CodeActionParams>(request.params) {
                                Ok(params) => match actions.for_request(&params) {
                                    Ok(actions) => Response::new_ok(request.id, actions),
                                    Err(error) => Response::new_err(request.id, -32603, error.to_string()),
                                },
                                Err(error) => Response::new_err(request.id, -32602, error.to_string()),
                            }
                        } else {
                            Response::new_err(request.id, -32601, format!("unsupported method: {}", request.method))
                        };
                        connection.sender.send(Message::Response(response))?;
                    }
                    Message::Notification(notification) => {
                        if notification.method == "exit" { return Err("exit received before shutdown".into()); }
                        if let Err(error) = notify(connection, &mut session, &mut pending, &mut actions, notification) {
                            show_error(connection, error)?;
                        }
                    }
                    Message::Response(_) => {}
                }
            }
            recv(results) -> completed => {
                busy = false;
                let completed = completed.map_err(|_| "analysis worker disconnected")?;
                if let Some(publication) = session.publish(completed)
                    && let Err(error) = publish_analysis(connection, &mut actions, publication)
                {
                    show_error(connection, error)?;
                }
            }
        }
    }
}

fn show_error(
    connection: &Connection,
    error: Box<dyn std::error::Error + Send + Sync>,
) -> Result<()> {
    connection
        .sender
        .send(Message::Notification(Notification::new(
            ShowMessage::METHOD.into(),
            lsp::ShowMessageParams {
                typ: lsp::MessageType::ERROR,
                message: error.to_string(),
            },
        )))?;
    Ok(())
}

fn notify(
    connection: &Connection,
    session: &mut Session,
    pending: &mut BTreeMap<String, Capture>,
    actions: &mut ActionCache,
    notification: Notification,
) -> Result<()> {
    let uri = match notification.method.as_str() {
        "textDocument/didOpen" => {
            let params: lsp::DidOpenTextDocumentParams =
                serde_json::from_value(notification.params)?;
            let document = params.text_document;
            session.open(
                document.uri.as_str().into(),
                i64::from(document.version),
                document.text,
            )?;
            document.uri
        }
        "textDocument/didChange" => {
            let params: lsp::DidChangeTextDocumentParams =
                serde_json::from_value(notification.params)?;
            let changes = params
                .content_changes
                .into_iter()
                .map(|change| match change.range {
                    None => Ok(Change::Replace(change.text)),
                    Some(range) => Ok(Change::Range {
                        start: position(range.start)?,
                        end: position(range.end)?,
                        text: change.text,
                    }),
                })
                .collect::<Result<Vec<_>>>()?;
            session.change(
                params.text_document.uri.as_str(),
                i64::from(params.text_document.version),
                changes,
                Encoding::Utf16,
            )?;
            params.text_document.uri
        }
        "textDocument/didClose" => {
            let params: lsp::DidCloseTextDocumentParams =
                serde_json::from_value(notification.params)?;
            pending.remove(params.text_document.uri.as_str());
            actions.invalidate(params.text_document.uri.as_str());
            if session.close(params.text_document.uri.as_str()) {
                publish(
                    connection,
                    lsp::PublishDiagnosticsParams::new(params.text_document.uri, Vec::new(), None),
                )?;
            }
            return Ok(());
        }
        _ => return Ok(()),
    };
    actions.invalidate(uri.as_str());
    pending.insert(uri.as_str().into(), session.capture(uri.as_str())?);
    Ok(())
}

fn publish_analysis(
    connection: &Connection,
    actions: &mut ActionCache,
    publication: Publication,
) -> Result<()> {
    let uri = publication.uri.parse()?;
    let proposals = publication.run.analysis.diagnostics.clone();
    let diagnostics = publication
        .run
        .analysis
        .diagnostics
        .into_iter()
        .map(|diagnostic| {
            let related_information = diagnostic
                .related
                .into_iter()
                .map(|related| {
                    Ok(lsp::DiagnosticRelatedInformation {
                        location: lsp::Location {
                            uri: related.location.source().uri().parse()?,
                            range: range(&related.location)?,
                        },
                        message: related.message,
                    })
                })
                .collect::<Result<Vec<_>>>()?;
            Ok(lsp::Diagnostic {
                range: range(&diagnostic.primary)?,
                severity: Some(match diagnostic.severity {
                    Severity::Error => lsp::DiagnosticSeverity::ERROR,
                    Severity::Warning => lsp::DiagnosticSeverity::WARNING,
                    Severity::Information => lsp::DiagnosticSeverity::INFORMATION,
                    Severity::Hint => lsp::DiagnosticSeverity::HINT,
                }),
                code: Some(lsp::NumberOrString::String(diagnostic.rule)),
                source: Some("actionlint".into()),
                message: diagnostic.message,
                related_information: Some(related_information),
                ..Default::default()
            })
        })
        .collect::<Result<Vec<_>>>()?;
    publish(
        connection,
        lsp::PublishDiagnosticsParams::new(
            uri,
            diagnostics,
            Some(i32::try_from(publication.version)?),
        ),
    )?;
    actions.record(
        publication.uri.clone(),
        i32::try_from(publication.version)?,
        proposals,
    );
    if !publication.run.failures.is_empty()
        || !matches!(publication.run.analysis.completion, Completion::Complete)
    {
        connection
            .sender
            .send(Message::Notification(Notification::new(
                ShowMessage::METHOD.into(),
                lsp::ShowMessageParams {
                    typ: lsp::MessageType::WARNING,
                    message: format!(
                        "{}: analysis incomplete: {:?}; {:?}",
                        publication.uri,
                        publication.run.analysis.completion,
                        publication.run.failures
                    ),
                },
            )))?;
    }
    Ok(())
}

fn publish(connection: &Connection, params: lsp::PublishDiagnosticsParams) -> Result<()> {
    connection
        .sender
        .send(Message::Notification(Notification::new(
            PublishDiagnostics::METHOD.into(),
            params,
        )))?;
    Ok(())
}

fn position(position: lsp::Position) -> Result<Position> {
    Ok(Position {
        line: usize::try_from(position.line)?,
        character: usize::try_from(position.character)?,
    })
}

fn range(span: &Span) -> Result<lsp::Range> {
    let convert = |offset| -> Result<lsp::Position> {
        let position = span.source().position(offset, Encoding::Utf16)?;
        Ok(lsp::Position::new(
            u32::try_from(position.line)?,
            u32::try_from(position.character)?,
        ))
    };
    let bytes = span.bytes();
    Ok(lsp::Range::new(convert(bytes.start)?, convert(bytes.end)?))
}

#[cfg(test)]
mod scheduler_tests;
