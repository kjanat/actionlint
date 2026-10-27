use crate::{analysis::Fact, source::Span};

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Name {
    pub value: String,
    pub location: Span,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Job {
    pub name: Name,
    pub needs: Fact<Vec<Name>>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Workflow {
    pub jobs: Fact<Vec<Job>>,
}
