//! Immutable source revisions and checked coordinates shared by
//! analysis and editors.

use std::{fmt, ops::Range, sync::Arc};

mod edit;
pub use edit::{Edit, EditError};
mod mapped;
pub use mapped::{MappedText, Origin};

#[derive(Debug, PartialEq, Eq)]
struct Text {
    uri: String,
    content: String,
    lines: Vec<usize>,
}

/// Equality compares document identity and complete content, including line endings.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Snapshot(Arc<Text>);

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Encoding {
    Utf8,
    Utf16,
    UnicodeScalar,
}

/// Zero-based line and character units in the requested encoding.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Position {
    pub line: usize,
    pub character: usize,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Span {
    source: Snapshot,
    bytes: Range<usize>,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CoordinateError {
    OutOfBounds,
    SplitCharacter,
    InsideLineEnding,
    ReversedRange,
}

impl fmt::Display for CoordinateError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Self::OutOfBounds => "source coordinate is outside the document",
            Self::SplitCharacter => "source coordinate splits a character",
            Self::InsideLineEnding => "source coordinate splits a line ending",
            Self::ReversedRange => "source range ends before it starts",
        })
    }
}

impl std::error::Error for CoordinateError {}

impl Encoding {
    fn width(self, character: char) -> usize {
        match self {
            Self::Utf8 => character.len_utf8(),
            Self::Utf16 => character.len_utf16(),
            Self::UnicodeScalar => 1,
        }
    }
}

impl Snapshot {
    pub fn new(uri: String, content: String) -> Self {
        let mut lines = vec![0];
        let mut bytes = content.bytes().enumerate().peekable();
        while let Some((index, byte)) = bytes.next() {
            match byte {
                b'\r' => {
                    let end = if bytes.peek().is_some_and(|(_, next)| *next == b'\n') {
                        bytes.next();
                        index + 2
                    } else {
                        index + 1
                    };
                    lines.push(end);
                }
                b'\n' => lines.push(index + 1),
                _ => {}
            }
        }
        Self(Arc::new(Text {
            uri,
            content,
            lines,
        }))
    }

    pub fn uri(&self) -> &str {
        &self.0.uri
    }
    pub fn text(&self) -> &str {
        &self.0.content
    }
    pub fn line_count(&self) -> usize {
        self.0.lines.len()
    }

    fn line_bounds(&self, line: usize) -> Result<Range<usize>, CoordinateError> {
        let start = *self.0.lines.get(line).ok_or(CoordinateError::OutOfBounds)?;
        let mut end = self
            .0
            .lines
            .get(line + 1)
            .copied()
            .unwrap_or(self.text().len());
        if end > start && self.text().as_bytes()[end - 1] == b'\n' {
            end -= 1;
        }
        if end > start && self.text().as_bytes()[end - 1] == b'\r' {
            end -= 1;
        }
        Ok(start..end)
    }

    pub fn line(&self, line: usize) -> Result<&str, CoordinateError> {
        Ok(&self.text()[self.line_bounds(line)?])
    }

    pub fn offset(&self, position: Position, encoding: Encoding) -> Result<usize, CoordinateError> {
        let bounds = self.line_bounds(position.line)?;
        let text = &self.text()[bounds.clone()];
        let mut units = 0;
        for (offset, character) in text.char_indices() {
            if units == position.character {
                return Ok(bounds.start + offset);
            }
            units += encoding.width(character);
            if units > position.character {
                return Err(CoordinateError::SplitCharacter);
            }
        }
        if units == position.character {
            Ok(bounds.end)
        } else {
            Err(CoordinateError::OutOfBounds)
        }
    }

    pub fn position(&self, offset: usize, encoding: Encoding) -> Result<Position, CoordinateError> {
        if offset > self.text().len() {
            return Err(CoordinateError::OutOfBounds);
        }
        if !self.text().is_char_boundary(offset) {
            return Err(CoordinateError::SplitCharacter);
        }
        let line = self.0.lines.partition_point(|start| *start <= offset) - 1;
        let bounds = self.line_bounds(line)?;
        if offset > bounds.end {
            return Err(CoordinateError::InsideLineEnding);
        }
        let character = self.text()[bounds.start..offset]
            .chars()
            .map(|c| encoding.width(c))
            .sum();
        Ok(Position { line, character })
    }

    pub fn span(&self, bytes: Range<usize>) -> Result<Span, CoordinateError> {
        if bytes.start > bytes.end {
            return Err(CoordinateError::ReversedRange);
        }
        if bytes.end > self.text().len() {
            return Err(CoordinateError::OutOfBounds);
        }
        if !self.text().is_char_boundary(bytes.start) || !self.text().is_char_boundary(bytes.end) {
            return Err(CoordinateError::SplitCharacter);
        }
        Ok(Span {
            source: self.clone(),
            bytes,
        })
    }
}

impl Span {
    pub fn source(&self) -> &Snapshot {
        &self.source
    }
    pub fn bytes(&self) -> Range<usize> {
        self.bytes.clone()
    }
    pub fn text(&self) -> &str {
        &self.source.text()[self.bytes.clone()]
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn source(text: &str) -> Snapshot {
        Snapshot::new("file:///workflow.yml".into(), text.into())
    }

    #[test]
    fn editor_encodings_round_trip_without_splitting_surrogates() {
        let snapshot = source("a🦀é\r\nnext");
        for (encoding, column) in [
            (Encoding::Utf8, 5),
            (Encoding::Utf16, 3),
            (Encoding::UnicodeScalar, 2),
        ] {
            let position = Position {
                line: 0,
                character: column,
            };
            assert_eq!(snapshot.offset(position, encoding), Ok(5));
            assert_eq!(snapshot.position(5, encoding), Ok(position));
        }
        assert_eq!(
            snapshot.offset(
                Position {
                    line: 0,
                    character: 2
                },
                Encoding::Utf16
            ),
            Err(CoordinateError::SplitCharacter)
        );
        assert_eq!(
            snapshot.position(2, Encoding::Utf8),
            Err(CoordinateError::SplitCharacter)
        );
    }

    #[test]
    fn line_endings_and_final_empty_line_are_explicit() {
        let snapshot = source("a\r\nb\rc\n");
        assert_eq!(snapshot.line_count(), 4);
        for (index, expected) in ["a", "b", "c", ""].into_iter().enumerate() {
            assert_eq!(snapshot.line(index), Ok(expected));
        }
        assert_eq!(
            snapshot.position(2, Encoding::Utf16),
            Err(CoordinateError::InsideLineEnding)
        );
        assert_eq!(
            snapshot.position(7, Encoding::Utf16),
            Ok(Position {
                line: 3,
                character: 0
            })
        );
        assert_eq!(
            snapshot.offset(
                Position {
                    line: 4,
                    character: 0
                },
                Encoding::Utf8
            ),
            Err(CoordinateError::OutOfBounds)
        );
    }

    #[test]
    fn locations_own_the_captured_revision() {
        let captured = source("old");
        let span = captured.span(0..3).unwrap();
        drop(captured);
        let current = source("new");
        assert_eq!(span.text(), "old");
        assert_ne!(span.source(), &current);
        assert_ne!(
            source("old"),
            Snapshot::new("file:///other.yml".into(), "old".into())
        );
        assert_eq!(span.source(), &source("old"));
    }

    #[test]
    fn empty_document_and_invalid_ranges() {
        let snapshot = source("");
        assert_eq!(
            snapshot.position(0, Encoding::Utf16),
            Ok(Position {
                line: 0,
                character: 0
            })
        );
        assert_eq!(snapshot.span(0..0).unwrap().text(), "");
        assert_eq!(snapshot.span(0..1), Err(CoordinateError::OutOfBounds));
        assert_eq!(
            snapshot.span(Range { start: 1, end: 0 }),
            Err(CoordinateError::ReversedRange)
        );
    }
}
