//! Component execution with generated contracts and per-call runtime state.

use wasmtime::component::{Component, Linker};
use wasmtime::{Config, Engine, Store, StoreLimits, StoreLimitsBuilder};

mod workflow;

pub mod bindings {
    wasmtime::component::bindgen!({ path: "../../wit", world: "analyzer" });
}

use bindings::actionlint::extension::types::{AnalysisResult, Completion, Failure, Request};

pub struct ComponentHost {
    engine: Engine,
}

pub struct CompiledComponent {
    engine: Engine,
    component: Component,
}

impl ComponentHost {
    pub fn new() -> wasmtime::Result<Self> {
        let mut config = Config::new();
        config.consume_fuel(true);
        Ok(Self {
            engine: Engine::new(&config)?,
        })
    }

    pub fn compile(&self, bytes: &[u8]) -> wasmtime::Result<CompiledComponent> {
        Ok(CompiledComponent {
            engine: self.engine.clone(),
            component: Component::new(&self.engine, bytes)?,
        })
    }
}

impl CompiledComponent {
    pub fn analyze(&self, request: &Request) -> wasmtime::Result<Result<AnalysisResult, Failure>> {
        let limits = StoreLimitsBuilder::new()
            .memory_size(64 * 1024 * 1024)
            .table_elements(10_000)
            .build();
        let mut store: Store<StoreLimits> = Store::new(&self.engine, limits);
        store.limiter(|limits| limits);
        store.set_fuel(10_000_000)?;
        let linker = Linker::new(&self.engine);
        let instance = bindings::Analyzer::instantiate(&mut store, &self.component, &linker)?;
        let result = instance
            .actionlint_extension_analysis()
            .call_analyze(&mut store, request)?;
        if let Ok(result) = &result {
            validate_result(request, result)?;
        }
        Ok(result)
    }
}

fn validate_result(request: &Request, result: &AnalysisResult) -> wasmtime::Result<()> {
    wasmtime::ensure!(
        result.snapshot == request.snapshot,
        "component returned another snapshot"
    );
    if let Completion::Incomplete(reasons) = &result.completion {
        wasmtime::ensure!(!reasons.is_empty(), "incomplete analysis has no reason");
    }
    for diagnostic in &result.diagnostics {
        validate_range(request, &diagnostic.primary)?;
        for related in &diagnostic.related {
            validate_range(request, &related.location)?;
        }
        for fix in &diagnostic.fixes {
            wasmtime::ensure!(!fix.edits.is_empty(), "component returned an empty fix");
            let mut sources = std::collections::BTreeMap::new();
            for edit in &fix.edits {
                validate_range(request, &edit.location)?;
                sources
                    .entry(edit.location.source)
                    .or_insert_with(Vec::new)
                    .push(edit);
            }
            for (id, edits) in sources {
                let input = request
                    .sources
                    .iter()
                    .find(|source| source.id == id)
                    .ok_or_else(|| {
                        wasmtime::format_err!("component returned an unknown source ID")
                    })?;
                let source =
                    actionlint_model::source::Snapshot::new(id.to_string(), input.text.clone());
                let edits = edits
                    .into_iter()
                    .map(|edit| {
                        let span = source.span(
                            usize::try_from(edit.location.start)?
                                ..usize::try_from(edit.location.end)?,
                        )?;
                        Ok(actionlint_model::source::Edit {
                            span,
                            replacement: edit.replacement.clone(),
                        })
                    })
                    .collect::<wasmtime::Result<Vec<_>>>()?;
                source.validate_edits(&edits)?;
            }
        }
    }
    Ok(())
}

fn validate_range(
    request: &Request,
    range: &bindings::actionlint::extension::types::Range,
) -> wasmtime::Result<()> {
    let source = request
        .sources
        .iter()
        .find(|source| source.id == range.source)
        .ok_or_else(|| wasmtime::format_err!("component returned an unknown source ID"))?;
    let start = usize::try_from(range.start)?;
    let end = usize::try_from(range.end)?;
    wasmtime::ensure!(
        start <= end && end <= source.text.len(),
        "component returned an invalid range"
    );
    wasmtime::ensure!(
        source.text.is_char_boundary(start) && source.text.is_char_boundary(end),
        "component range splits a UTF-8 character"
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use bindings::actionlint::extension::types::*;

    fn fixture() -> (Request, AnalysisResult) {
        let location = Range {
            source: 7,
            start: 0,
            end: 1,
        };
        (
            Request {
                expressions: Vec::new(),
                snapshot: 1,
                sources: vec![Source {
                    id: 7,
                    text: "a🦀z".into(),
                }],
                jobs: Jobs::Known(Vec::new()),
            },
            AnalysisResult {
                snapshot: 1,
                completion: Completion::Complete,
                diagnostics: vec![Diagnostic {
                    rule: "test/fix".into(),
                    message: "fix".into(),
                    severity: Severity::Warning,
                    primary: location,
                    related: vec![Related {
                        message: "context".into(),
                        location,
                    }],
                    fixes: vec![Fix {
                        title: "replace".into(),
                        applicability: Applicability::RequiresReview,
                        edits: vec![Edit {
                            location,
                            replacement: "b".into(),
                        }],
                    }],
                }],
            },
        )
    }

    #[test]
    fn related_locations_are_admitted_against_request_sources() {
        let (request, mut result) = fixture();
        validate_result(&request, &result).unwrap();
        result.diagnostics[0].related[0].location.source = 8;
        assert!(
            validate_result(&request, &result)
                .unwrap_err()
                .to_string()
                .contains("unknown source")
        );
        result.diagnostics[0].related[0].location = Range {
            source: 7,
            start: 2,
            end: 5,
        };
        assert!(
            validate_result(&request, &result)
                .unwrap_err()
                .to_string()
                .contains("UTF-8")
        );
    }

    #[test]
    fn fix_transactions_reject_overlap_but_allow_adjacent_edits() {
        let (request, mut result) = fixture();
        result.diagnostics[0].fixes[0].edits.push(Edit {
            location: Range {
                source: 7,
                start: 1,
                end: 5,
            },
            replacement: "crab".into(),
        });
        validate_result(&request, &result).unwrap();
        result.diagnostics[0].fixes[0].edits[1].location.start = 0;
        assert!(
            validate_result(&request, &result)
                .unwrap_err()
                .to_string()
                .contains("overlap")
        );
        result.diagnostics[0].fixes[0].edits.clear();
        assert!(
            validate_result(&request, &result)
                .unwrap_err()
                .to_string()
                .contains("empty fix")
        );
    }

    #[test]
    fn separate_fix_alternatives_may_change_the_same_range() {
        let (request, mut result) = fixture();
        let alternative = result.diagnostics[0].fixes[0].clone();
        result.diagnostics[0].fixes.push(alternative);
        validate_result(&request, &result).unwrap();
    }
}
