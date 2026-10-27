//! Analyzer composition and filesystem input for analysis frontends.

use actionlint_component_host::ComponentHost;
use actionlint_engine::{Analyzer, Run, WorkflowAnalyzer, analyze_workflow};
use actionlint_model::source::Snapshot;
use std::path::{Path, PathBuf};

pub mod config;
pub mod editor;

pub struct Application {
    analyzers: Vec<Registration>,
}

/// An owned analyzer shared by file checks and editor analysis workers.
pub struct Registration {
    name: String,
    implementation: Box<dyn WorkflowAnalyzer + Send + Sync>,
}

impl Registration {
    pub fn new(
        name: impl Into<String>,
        implementation: impl WorkflowAnalyzer + Send + Sync + 'static,
    ) -> Self {
        Self {
            name: name.into(),
            implementation: Box::new(implementation),
        }
    }
}

impl Application {
    pub fn new(analyzers: Vec<Registration>) -> Result<Self, String> {
        if analyzers.is_empty() {
            return Err("no analyzers selected".into());
        }
        let mut names = std::collections::BTreeSet::new();
        for analyzer in &analyzers {
            if analyzer.name.trim().is_empty() {
                return Err("analyzer name is empty".into());
            }
            if !names.insert(&analyzer.name) {
                return Err(format!("analyzer selected twice: {}", analyzer.name));
            }
        }
        Ok(Self { analyzers })
    }

    pub fn load(components: &[PathBuf]) -> Result<Self, String> {
        if components.is_empty() {
            return Err("no analyzer components selected".into());
        }
        let host = ComponentHost::new().map_err(|error| format!("{error:#}"))?;
        let mut analyzers = Vec::new();
        let mut selected = std::collections::BTreeSet::new();
        for component in components {
            let canonical = component
                .canonicalize()
                .map_err(|error| format!("{}: {error}", component.display()))?;
            if !selected.insert(canonical) {
                return Err(format!("component selected twice: {}", component.display()));
            }
            let bytes = std::fs::read(component)
                .map_err(|error| format!("{}: {error}", component.display()))?;
            let analyzer = host
                .compile(&bytes)
                .map_err(|error| format!("{}: {error:#}", component.display()))?;
            analyzers.push(Registration::new(component.display().to_string(), analyzer));
        }
        Self::new(analyzers)
    }

    pub fn analyze_snapshot(&self, source: Snapshot) -> Run {
        let analyzers: Vec<_> = self
            .analyzers
            .iter()
            .map(|analyzer| Analyzer {
                name: &analyzer.name,
                implementation: analyzer.implementation.as_ref(),
            })
            .collect();
        analyze_workflow(source, &analyzers)
    }

    pub fn check(&self, path: &Path) -> Result<Run, String> {
        let text = std::fs::read_to_string(path)
            .map_err(|error| format!("{}: {error}", path.display()))?;
        let source = Snapshot::new(path.display().to_string(), text);
        Ok(self.analyze_snapshot(source))
    }
}
