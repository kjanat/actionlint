use std::fmt;

use super::{Snapshot, Span};

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Edit {
    pub span: Span,
    pub replacement: String,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum EditError {
    WrongRevision,
    Overlap,
}

impl fmt::Display for EditError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Self::WrongRevision => "edit belongs to a different document or revision",
            Self::Overlap => "edits overlap or have ambiguous insertion order",
        })
    }
}

impl std::error::Error for EditError {}

impl Snapshot {
    fn ordered_edits<'a>(&self, edits: &'a [Edit]) -> Result<Vec<&'a Edit>, EditError> {
        let mut ordered: Vec<&Edit> = edits.iter().collect();
        if ordered.iter().any(|edit| edit.span.source() != self) {
            return Err(EditError::WrongRevision);
        }
        ordered.sort_by_key(|edit| (edit.span.bytes().start, edit.span.bytes().end));
        for pair in ordered.windows(2) {
            let left = pair[0].span.bytes();
            let right = pair[1].span.bytes();
            if left.end > right.start || left.start == right.start {
                return Err(EditError::Overlap);
            }
        }
        Ok(ordered)
    }

    pub fn validate_edits(&self, edits: &[Edit]) -> Result<(), EditError> {
        self.ordered_edits(edits).map(|_| ())
    }

    /// Apply one transaction against this captured text. Input order has no effect.
    pub fn apply(&self, edits: &[Edit]) -> Result<Self, EditError> {
        let ordered = self.ordered_edits(edits)?;
        let mut result = String::new();
        let mut offset = 0;
        for edit in ordered {
            let range = edit.span.bytes();
            result.push_str(&self.text()[offset..range.start]);
            result.push_str(&edit.replacement);
            offset = range.end;
        }
        result.push_str(&self.text()[offset..]);
        Ok(Self::new(self.uri().to_owned(), result))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn source(text: &str) -> Snapshot {
        Snapshot::new("file:///workflow.yml".into(), text.into())
    }

    #[test]
    fn edits_use_original_offsets_and_preserve_old_locations() {
        let snapshot = source("a🦀z");
        let original = snapshot.span(1..5).unwrap();
        let edits = [
            Edit {
                span: snapshot.span(5..6).unwrap(),
                replacement: "end".into(),
            },
            Edit {
                span: snapshot.span(0..1).unwrap(),
                replacement: String::new(),
            },
        ];
        let changed = snapshot.apply(&edits).unwrap();
        assert_eq!(changed.text(), "🦀end");
        assert_eq!(original.text(), "🦀");
        assert_eq!(snapshot.text(), "a🦀z");
        assert_eq!(changed.apply(&edits), Err(EditError::WrongRevision));
    }

    #[test]
    fn one_stale_edit_rejects_the_whole_transaction() {
        let snapshot = source("abcd");
        let stale = source("abce");
        let edits = [
            Edit {
                span: snapshot.span(0..1).unwrap(),
                replacement: "x".into(),
            },
            Edit {
                span: stale.span(3..4).unwrap(),
                replacement: "y".into(),
            },
        ];
        assert_eq!(snapshot.apply(&edits), Err(EditError::WrongRevision));
        assert_eq!(snapshot.text(), "abcd");
    }

    #[test]
    fn ambiguous_changes_are_rejected_and_adjacent_replacements_work() {
        let snapshot = source("abc");
        for ranges in [[0..2, 1..3], [1..1, 1..1], [1..1, 1..2]] {
            let edits = ranges.map(|range| Edit {
                span: snapshot.span(range).unwrap(),
                replacement: "x".into(),
            });
            assert_eq!(snapshot.apply(&edits), Err(EditError::Overlap));
        }
        let edits = [0..1, 1..3].map(|range| Edit {
            span: snapshot.span(range).unwrap(),
            replacement: "x".into(),
        });
        assert_eq!(snapshot.apply(&edits).unwrap().text(), "xx");
    }

    #[test]
    fn identical_text_in_another_document_is_not_the_same_revision() {
        let snapshot = source("text");
        let other = Snapshot::new("file:///other.yml".into(), "text".into());
        let edit = Edit {
            span: other.span(0..4).unwrap(),
            replacement: "new".into(),
        };
        assert_eq!(snapshot.apply(&[edit]), Err(EditError::WrongRevision));
    }
}
