use super::{CoordinateError, Span};
use std::ops::Range;

/// Exact origins preserve byte offsets; transformed origins identify the entire
/// source construct responsible for the selected decoded text.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Origin {
    Exact(Span),
    Transformed(Span),
    Generated,
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct Part {
    decoded: Range<usize>,
    origin: Origin,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct MappedText {
    text: String,
    parts: Vec<Part>,
}

impl MappedText {
    pub fn segments(&self) -> impl Iterator<Item = (&str, &Origin)> {
        self.parts
            .iter()
            .map(|part| (&self.text[part.decoded.clone()], &part.origin))
    }
    pub fn text(&self) -> &str {
        &self.text
    }

    pub fn copy(&mut self, source: Span) {
        let start = self.text.len();
        self.text.push_str(source.text());
        self.parts.push(Part {
            decoded: start..self.text.len(),
            origin: Origin::Exact(source),
        });
    }

    pub fn transform(&mut self, text: &str, source: Span) {
        self.append(text, Origin::Transformed(source));
    }

    pub fn generate(&mut self, text: &str) {
        self.append(text, Origin::Generated);
    }

    fn append(&mut self, text: &str, origin: Origin) {
        let start = self.text.len();
        self.text.push_str(text);
        self.parts.push(Part {
            decoded: start..self.text.len(),
            origin,
        });
    }

    /// Empty selections have no origin. Insertion locations require a separate
    /// boundary decision; they cannot be inferred from adjacent text.
    pub fn origins(&self, selected: Range<usize>) -> Result<Vec<Origin>, CoordinateError> {
        Ok(self
            .slice(selected)?
            .parts
            .into_iter()
            .map(|part| part.origin)
            .collect())
    }

    pub fn slice(&self, selected: Range<usize>) -> Result<Self, CoordinateError> {
        if selected.start > selected.end {
            return Err(CoordinateError::ReversedRange);
        }
        if selected.end > self.text.len() {
            return Err(CoordinateError::OutOfBounds);
        }
        if !self.text.is_char_boundary(selected.start) || !self.text.is_char_boundary(selected.end)
        {
            return Err(CoordinateError::SplitCharacter);
        }
        if selected.is_empty() {
            return Ok(Self::default());
        }
        let mut result = Self::default();
        for part in &self.parts {
            let start = selected.start.max(part.decoded.start);
            let end = selected.end.min(part.decoded.end);
            if start >= end {
                continue;
            }
            let origin =
                match &part.origin {
                    Origin::Exact(source) => {
                        let base = source.bytes().start;
                        Origin::Exact(source.source().span(
                            base + start - part.decoded.start..base + end - part.decoded.start,
                        )?)
                    }
                    other => other.clone(),
                };
            result.append(&self.text[start..end], origin);
        }
        Ok(result)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::source::Snapshot;

    #[test]
    fn mixed_text_preserves_revision_and_transformation_boundaries() {
        let source = Snapshot::new("workflow.yml".into(), "🦀\\nend".into());
        let mut text = MappedText::default();
        text.copy(source.span(0..4).unwrap());
        text.transform("\n", source.span(4..6).unwrap());
        text.generate(" ");
        text.copy(source.span(6..9).unwrap());
        assert_eq!(text.text(), "🦀\n end");
        assert_eq!(
            text.origins(4..8).unwrap(),
            vec![
                Origin::Transformed(source.span(4..6).unwrap()),
                Origin::Generated,
                Origin::Exact(source.span(6..8).unwrap()),
            ]
        );
        assert_eq!(text.origins(1..4), Err(CoordinateError::SplitCharacter));
        assert_eq!(text.origins(0..100), Err(CoordinateError::OutOfBounds));
        assert_eq!(
            text.origins(Range { start: 4, end: 0 }),
            Err(CoordinateError::ReversedRange)
        );
        assert!(text.origins(4..4).unwrap().is_empty());
    }

    #[test]
    fn adjacent_decoded_segments_do_not_merge_disjoint_sources() {
        let first = Snapshot::new("one.yml".into(), "first".into());
        let second = Snapshot::new("two.yml".into(), "second".into());
        let mut text = MappedText::default();
        text.copy(first.span(0..5).unwrap());
        text.copy(second.span(0..6).unwrap());
        assert_eq!(
            text.origins(3..8).unwrap(),
            vec![
                Origin::Exact(first.span(3..5).unwrap()),
                Origin::Exact(second.span(0..3).unwrap()),
            ]
        );
    }
}
