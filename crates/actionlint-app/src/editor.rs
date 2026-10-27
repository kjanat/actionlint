//! Versioned editor buffers and admission of asynchronous analysis results.

use crate::Application;
use actionlint_engine::Run;
use actionlint_model::source::{CoordinateError, Edit, EditError, Encoding, Position, Snapshot};
use std::{collections::BTreeMap, fmt, sync::Arc};

pub enum Change {
    Replace(String),
    Range {
        start: Position,
        end: Position,
        text: String,
    },
}

#[derive(Debug, PartialEq, Eq)]
pub enum BufferError {
    AlreadyOpen,
    NotOpen,
    StaleVersion,
    Coordinate(CoordinateError),
    Edit(EditError),
}

impl fmt::Display for BufferError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::AlreadyOpen => f.write_str("document is already open"),
            Self::NotOpen => f.write_str("document is not open"),
            Self::StaleVersion => f.write_str("document version did not advance"),
            Self::Coordinate(error) => error.fmt(f),
            Self::Edit(error) => error.fmt(f),
        }
    }
}
impl std::error::Error for BufferError {}

struct Buffer {
    version: i64,
    source: Snapshot,
    incarnation: Arc<()>,
}

#[derive(Default)]
pub struct Session {
    buffers: BTreeMap<String, Buffer>,
}

pub struct Capture {
    version: i64,
    source: Snapshot,
    incarnation: Arc<()>,
}

pub struct Completed {
    capture: Capture,
    run: Run,
}

pub struct Publication {
    pub uri: String,
    pub version: i64,
    pub run: Run,
}

impl Capture {
    pub fn source(&self) -> &Snapshot {
        &self.source
    }

    pub fn analyze(self, application: &Application) -> Completed {
        let run = application.analyze_snapshot(self.source.clone());
        Completed { capture: self, run }
    }
}

impl Session {
    pub fn open(&mut self, uri: String, version: i64, text: String) -> Result<(), BufferError> {
        if self.buffers.contains_key(&uri) {
            return Err(BufferError::AlreadyOpen);
        }
        self.buffers.insert(
            uri.clone(),
            Buffer {
                version,
                source: Snapshot::new(uri, text),
                incarnation: Arc::new(()),
            },
        );
        Ok(())
    }

    pub fn change(
        &mut self,
        uri: &str,
        version: i64,
        changes: Vec<Change>,
        encoding: Encoding,
    ) -> Result<(), BufferError> {
        let buffer = self.buffers.get_mut(uri).ok_or(BufferError::NotOpen)?;
        if version <= buffer.version {
            return Err(BufferError::StaleVersion);
        }
        let mut source = buffer.source.clone();
        // Each range addresses the result of the preceding change in this notification.
        for change in changes {
            source = match change {
                Change::Replace(text) => Snapshot::new(uri.into(), text),
                Change::Range { start, end, text } => {
                    let start = source
                        .offset(start, encoding)
                        .map_err(BufferError::Coordinate)?;
                    let end = source
                        .offset(end, encoding)
                        .map_err(BufferError::Coordinate)?;
                    let span = source.span(start..end).map_err(BufferError::Coordinate)?;
                    source
                        .apply(&[Edit {
                            span,
                            replacement: text,
                        }])
                        .map_err(BufferError::Edit)?
                }
            };
        }
        buffer.source = source;
        buffer.version = version;
        Ok(())
    }

    pub fn close(&mut self, uri: &str) -> bool {
        self.buffers.remove(uri).is_some()
    }

    pub fn capture(&self, uri: &str) -> Result<Capture, BufferError> {
        let buffer = self.buffers.get(uri).ok_or(BufferError::NotOpen)?;
        Ok(Capture {
            version: buffer.version,
            source: buffer.source.clone(),
            incarnation: buffer.incarnation.clone(),
        })
    }

    pub fn publish(&self, completed: Completed) -> Option<Publication> {
        let capture = completed.capture;
        let buffer = self.buffers.get(capture.source.uri())?;
        if buffer.version != capture.version
            || !Arc::ptr_eq(&buffer.incarnation, &capture.incarnation)
        {
            return None;
        }
        Some(Publication {
            uri: capture.source.uri().into(),
            version: capture.version,
            run: completed.run,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use actionlint_model::analysis::{Analysis, Completion};

    fn complete(capture: Capture) -> Completed {
        Completed {
            capture,
            run: Run {
                analysis: Analysis {
                    diagnostics: Vec::new(),
                    completion: Completion::Complete,
                },
                failures: Vec::new(),
            },
        }
    }

    fn session() -> Session {
        let mut session = Session::default();
        session
            .open("file:///workflow.yml".into(), 1, "a🦀z\r\n".into())
            .unwrap();
        session
    }

    #[test]
    fn utf16_changes_are_sequential_and_old_capture_is_immutable() {
        let mut session = session();
        let old = session.capture("file:///workflow.yml").unwrap();
        session
            .change(
                "file:///workflow.yml",
                2,
                vec![
                    Change::Range {
                        start: Position {
                            line: 0,
                            character: 1,
                        },
                        end: Position {
                            line: 0,
                            character: 3,
                        },
                        text: "xy".into(),
                    },
                    Change::Range {
                        start: Position {
                            line: 0,
                            character: 2,
                        },
                        end: Position {
                            line: 0,
                            character: 3,
                        },
                        text: "!".into(),
                    },
                ],
                Encoding::Utf16,
            )
            .unwrap();
        assert_eq!(old.source().text(), "a🦀z\r\n");
        assert_eq!(
            session
                .capture("file:///workflow.yml")
                .unwrap()
                .source()
                .text(),
            "ax!z\r\n"
        );
        assert!(session.publish(complete(old)).is_none());
    }

    #[test]
    fn invalid_later_change_rolls_back_entire_notification() {
        let mut session = session();
        let old = session.capture("file:///workflow.yml").unwrap();
        let error = session
            .change(
                "file:///workflow.yml",
                2,
                vec![
                    Change::Replace("new".into()),
                    Change::Range {
                        start: Position {
                            line: 99,
                            character: 0,
                        },
                        end: Position {
                            line: 99,
                            character: 1,
                        },
                        text: "!".into(),
                    },
                ],
                Encoding::Utf16,
            )
            .unwrap_err();
        assert_eq!(error, BufferError::Coordinate(CoordinateError::OutOfBounds));
        assert_eq!(
            session
                .capture("file:///workflow.yml")
                .unwrap()
                .source()
                .text(),
            "a🦀z\r\n"
        );
        assert!(session.publish(complete(old)).is_some());
    }

    #[test]
    fn closing_and_reopening_same_version_invalidates_old_results() {
        let mut session = session();
        let old = session.capture("file:///workflow.yml").unwrap();
        assert!(session.close("file:///workflow.yml"));
        session
            .open("file:///workflow.yml".into(), 1, "a🦀z\r\n".into())
            .unwrap();
        assert!(session.publish(complete(old)).is_none());
        let current = session.capture("file:///workflow.yml").unwrap();
        assert_eq!(session.publish(complete(current)).unwrap().version, 1);
    }

    #[test]
    fn stale_versions_and_split_surrogates_do_not_mutate_buffer() {
        let mut session = session();
        assert_eq!(
            session.change(
                "file:///workflow.yml",
                1,
                vec![Change::Replace("bad".into())],
                Encoding::Utf16
            ),
            Err(BufferError::StaleVersion)
        );
        let error = session
            .change(
                "file:///workflow.yml",
                2,
                vec![Change::Range {
                    start: Position {
                        line: 0,
                        character: 2,
                    },
                    end: Position {
                        line: 0,
                        character: 3,
                    },
                    text: "bad".into(),
                }],
                Encoding::Utf16,
            )
            .unwrap_err();
        assert_eq!(
            error,
            BufferError::Coordinate(CoordinateError::SplitCharacter)
        );
        assert_eq!(
            session
                .capture("file:///workflow.yml")
                .unwrap()
                .source()
                .text(),
            "a🦀z\r\n"
        );
    }
}
