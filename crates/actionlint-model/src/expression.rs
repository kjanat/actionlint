use crate::source::{MappedText, Span};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ExpressionKind {
    Delimited,
    BareCondition,
    Unterminated,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Expression {
    pub kind: ExpressionKind,
    pub location: Span,
    pub text: MappedText,
}
