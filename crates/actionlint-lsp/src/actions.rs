use crate::{Result, range};
use actionlint_model::analysis::Diagnostic;
use lsp_types as lsp;
use std::collections::BTreeMap;

struct Proposals {
    version: i32,
    diagnostics: Vec<Diagnostic>,
}

#[derive(Default)]
pub(super) struct ActionCache(BTreeMap<String, Proposals>);

impl ActionCache {
    pub fn record(&mut self, uri: String, version: i32, diagnostics: Vec<Diagnostic>) {
        self.0.insert(
            uri,
            Proposals {
                version,
                diagnostics,
            },
        );
    }

    pub fn invalidate(&mut self, uri: &str) {
        self.0.remove(uri);
    }

    pub fn for_request(&self, params: &lsp::CodeActionParams) -> Result<Vec<lsp::CodeAction>> {
        if let Some(kinds) = &params.context.only
            && !kinds.iter().any(|kind| {
                kind == &lsp::CodeActionKind::QUICKFIX || kind == &lsp::CodeActionKind::EMPTY
            })
        {
            return Ok(Vec::new());
        }
        let uri = &params.text_document.uri;
        let Some(proposals) = self.0.get(uri.as_str()) else {
            return Ok(Vec::new());
        };
        let mut actions = Vec::new();
        for diagnostic in &proposals.diagnostics {
            let location = range(&diagnostic.primary)?;
            if !intersects(location, params.range) {
                continue;
            }
            for fix in &diagnostic.fixes {
                let mut edits = Vec::new();
                for edit in &fix.edits {
                    if edit.span.source().uri() != uri.as_str() {
                        return Err(
                            "fix targets a document without a captured editor version".into()
                        );
                    }
                    edits.push(lsp::OneOf::Left(lsp::TextEdit {
                        range: range(&edit.span)?,
                        new_text: edit.replacement.clone(),
                    }));
                }
                actions.push(lsp::CodeAction {
                    title: fix.title.clone(),
                    kind: Some(lsp::CodeActionKind::QUICKFIX),
                    edit: Some(lsp::WorkspaceEdit {
                        document_changes: Some(lsp::DocumentChanges::Edits(vec![
                            lsp::TextDocumentEdit {
                                text_document: lsp::OptionalVersionedTextDocumentIdentifier::new(
                                    uri.clone(),
                                    proposals.version,
                                ),
                                edits,
                            },
                        ])),
                        ..Default::default()
                    }),
                    ..Default::default()
                });
            }
        }
        Ok(actions)
    }
}

fn intersects(left: lsp::Range, right: lsp::Range) -> bool {
    if right.start == right.end {
        left.start <= right.start && right.start <= left.end
    } else if left.start == left.end {
        right.start <= left.start && left.start < right.end
    } else {
        left.start < right.end && right.start < left.end
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use actionlint_model::{
        analysis::{Applicability, Fix, Severity},
        source::{Edit, Snapshot},
    };

    fn proposals() -> ActionCache {
        let source = Snapshot::new("file:///workflow.yml".into(), "🦀bad".into());
        let span = source.span(4..7).unwrap();
        let mut cache = ActionCache::default();
        cache.record(
            source.uri().into(),
            7,
            vec![Diagnostic {
                rule: "test/fix".into(),
                message: "replace".into(),
                severity: Severity::Warning,
                primary: span.clone(),
                related: Vec::new(),
                fixes: vec![Fix {
                    title: "Replace text".into(),
                    applicability: Applicability::RequiresReview,
                    edits: vec![Edit {
                        span,
                        replacement: "good".into(),
                    }],
                }],
            }],
        );
        cache
    }

    fn request() -> lsp::CodeActionParams {
        serde_json::from_value(serde_json::json!({
            "textDocument": { "uri": "file:///workflow.yml" },
            "range": { "start": { "line": 0, "character": 2 }, "end": { "line": 0, "character": 5 } },
            "context": { "diagnostics": [] }
        })).unwrap()
    }

    #[test]
    fn edits_carry_version_and_utf16_ranges() {
        let actions = proposals().for_request(&request()).unwrap();
        assert_eq!(actions.len(), 1);
        let Some(lsp::DocumentChanges::Edits(documents)) =
            &actions[0].edit.as_ref().unwrap().document_changes
        else {
            panic!("expected document edits");
        };
        assert_eq!(documents[0].text_document.version, Some(7));
        let lsp::OneOf::Left(edit) = &documents[0].edits[0] else {
            panic!("unexpected annotated edit");
        };
        assert_eq!(edit.range.start.character, 2);
        assert_eq!(edit.range.end.character, 5);
        assert_eq!(edit.new_text, "good");
        assert!(actions[0].is_preferred.is_none());
    }

    #[test]
    fn range_and_kind_filter_and_invalidation_drop_proposals() {
        let mut cache = proposals();
        let mut params = request();
        params.context.only = Some(vec![lsp::CodeActionKind::REFACTOR]);
        assert!(cache.for_request(&params).unwrap().is_empty());
        params.context.only = None;
        params.range = lsp::Range::new(lsp::Position::new(1, 0), lsp::Position::new(1, 1));
        assert!(cache.for_request(&params).unwrap().is_empty());
        cache.invalidate("file:///workflow.yml");
        assert!(cache.for_request(&request()).unwrap().is_empty());
    }
    #[test]
    fn change_and_close_notifications_invalidate_cached_fixes() {
        for method in ["textDocument/didChange", "textDocument/didClose"] {
            let (server, _client) = lsp_server::Connection::memory();
            let mut session = actionlint_app::editor::Session::default();
            session
                .open("file:///workflow.yml".into(), 7, "🦀bad".into())
                .unwrap();
            let mut pending = BTreeMap::new();
            let mut cache = proposals();
            assert_eq!(cache.for_request(&request()).unwrap().len(), 1);
            let params = if method == "textDocument/didChange" {
                serde_json::json!({"textDocument": {"uri": "file:///workflow.yml", "version": 8}, "contentChanges": [{"text": "new"}]})
            } else {
                serde_json::json!({"textDocument": {"uri": "file:///workflow.yml"}})
            };
            crate::notify(
                &server,
                &mut session,
                &mut pending,
                &mut cache,
                lsp_server::Notification::new(method.into(), params),
            )
            .unwrap();
            assert!(cache.for_request(&request()).unwrap().is_empty());
        }
    }
}
