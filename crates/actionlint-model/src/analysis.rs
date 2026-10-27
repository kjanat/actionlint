use crate::source::{Edit, EditError, Snapshot, Span};

pub struct Input<'a> {
    pub source: &'a Snapshot,
    pub workflow: &'a crate::workflow::Workflow,
    pub expressions: &'a [crate::expression::Expression],
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Unavailable {
    Unresolved,
    Invalid,
    Unsupported,
    Cancelled,
    BudgetExceeded,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Fact<T> {
    Known(T),
    Unavailable(Unavailable),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Severity {
    Error,
    Warning,
    Information,
    Hint,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Diagnostic {
    pub rule: String,
    pub message: String,
    pub severity: Severity,
    pub primary: Span,
    pub related: Vec<Related>,
    pub fixes: Vec<Fix>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Completion {
    Complete,
    Incomplete(Vec<Unavailable>),
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Analysis {
    pub diagnostics: Vec<Diagnostic>,
    pub completion: Completion,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Related {
    pub message: String,
    pub location: Span,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Applicability {
    Automatic,
    RequiresReview,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Fix {
    pub title: String,
    pub applicability: Applicability,
    pub edits: Vec<Edit>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum AdmissionError {
    UnknownRevision,
    EmptyFix,
    Edit(EditError),
}

impl std::fmt::Display for AdmissionError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::UnknownRevision => {
                f.write_str("diagnostic references an unavailable source revision")
            }
            Self::EmptyFix => f.write_str("diagnostic contains an empty fix"),
            Self::Edit(error) => error.fmt(f),
        }
    }
}
impl std::error::Error for AdmissionError {}

impl Diagnostic {
    pub fn validate(&self, sources: &[Snapshot]) -> Result<(), AdmissionError> {
        let known = |span: &Span| {
            if sources.contains(span.source()) {
                Ok(())
            } else {
                Err(AdmissionError::UnknownRevision)
            }
        };
        known(&self.primary)?;
        for related in &self.related {
            known(&related.location)?;
        }
        for fix in &self.fixes {
            if fix.edits.is_empty() {
                return Err(AdmissionError::EmptyFix);
            }
            for edit in &fix.edits {
                known(&edit.span)?;
            }
            for source in sources {
                let edits: Vec<_> = fix
                    .edits
                    .iter()
                    .filter(|edit| edit.span.source() == source)
                    .cloned()
                    .collect();
                source
                    .validate_edits(&edits)
                    .map_err(AdmissionError::Edit)?;
            }
        }
        Ok(())
    }
}
