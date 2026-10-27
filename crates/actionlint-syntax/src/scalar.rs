use actionlint_model::{
    analysis::Unavailable,
    source::{MappedText, Span},
};
use yaml_rust2::{Yaml, YamlLoader};
mod block;

pub(crate) fn parent_indent(node: tree_sitter::Node<'_>) -> usize {
    let mut parent = node.parent();
    while let Some(node) = parent {
        if matches!(node.kind(), "block_mapping" | "block_sequence") {
            return node.start_position().column;
        }
        parent = node.parent();
    }
    0
}

/// Decode a scalar without treating decoded offsets as source offsets.
pub fn decode_scalar(source: Span, parent_indent: usize) -> Result<MappedText, Unavailable> {
    let block = source.text().starts_with(['|', '>']);
    let input = if block {
        format!("{}scalar: {}", " ".repeat(parent_indent), source.text())
    } else {
        source.text().into()
    };
    let values = YamlLoader::load_from_str(&input).map_err(|_| Unavailable::Invalid)?;
    let [value] = values.as_slice() else {
        return Err(Unavailable::Invalid);
    };
    let value = if block {
        value
            .as_hash()
            .and_then(|entries| entries.get(&Yaml::String("scalar".into())))
            .ok_or(Unavailable::Invalid)?
    } else {
        value
    };
    let Yaml::String(value) = value else {
        return Err(Unavailable::Invalid);
    };
    let raw = source.text();
    let mut mapped = MappedText::default();
    if raw == value {
        mapped.copy(source);
    } else if let Some(quoted) = quoted_mapping(&source, value) {
        mapped = quoted;
    } else if block && let Some(lines) = block::mapping(&source, value, parent_indent) {
        mapped = lines;
    } else {
        // Ambiguous alignments retain provenance without inventing exact offsets.
        mapped.transform(value, source);
    }
    Ok(mapped)
}

fn quoted_mapping(source: &Span, decoded: &str) -> Option<MappedText> {
    let raw = source.text();
    let quote = *raw.as_bytes().first()?;
    if !matches!(quote, b'\'' | b'"')
        || raw.as_bytes().last() != Some(&quote)
        || raw.len() < 2
        || raw.contains(['\r', '\n', '\u{85}', '\u{2028}', '\u{2029}'])
    {
        return None;
    }
    let base = source.bytes().start;
    let span = |start, end| source.source().span(base + start..base + end).ok();
    let end = raw.len() - 1;
    let mut cursor = 1;
    let mut run = cursor;
    let mut result = MappedText::default();
    while cursor < end {
        let byte = raw.as_bytes()[cursor];
        let escaped = quote == b'"' && byte == b'\\'
            || quote == b'\'' && byte == b'\'' && raw.as_bytes().get(cursor + 1) == Some(&b'\'');
        if !escaped {
            cursor += raw.get(cursor..)?.chars().next()?.len_utf8();
            continue;
        }
        if run < cursor {
            result.copy(span(run, cursor)?);
        }
        let width = if quote == b'\'' {
            2
        } else {
            match raw.as_bytes().get(cursor + 1)? {
                b'x' => 4,
                b'u' => 6,
                b'U' => 10,
                _ => 2,
            }
        };
        if cursor + width > end {
            return None;
        }
        // The YAML decoder owns escape semantics; this scan only locates tokens.
        let character = decoded.get(result.text().len()..)?.chars().next()?;
        let mut buffer = [0; 4];
        result.transform(
            character.encode_utf8(&mut buffer),
            span(cursor, cursor + width)?,
        );
        cursor += width;
        run = cursor;
    }
    if run < end {
        result.copy(span(run, end)?);
    }
    (result.text() == decoded).then_some(result)
}

#[cfg(test)]
mod tests {
    use super::*;
    use actionlint_model::source::{Origin, Snapshot};

    #[test]
    fn quoted_unicode_has_exact_original_byte_ranges() {
        let source = Snapshot::new("workflow.yml".into(), "'🦀name'".into());
        let value = decode_scalar(source.span(0..source.text().len()).unwrap(), 0).unwrap();
        assert_eq!(value.text(), "🦀name");
        assert_eq!(
            value.origins(4..8).unwrap(),
            vec![Origin::Exact(source.span(5..9).unwrap())]
        );
    }

    #[test]
    fn escapes_map_to_complete_tokens_and_following_text_is_exact() {
        for (raw, expected, selected, token, following) in [
            ("\"a\\nb\"", "a\nb", 1..2, 2..4, 4..5),
            ("'it''s'", "it's", 2..3, 3..5, 5..6),
            ("\"\\U0001F980x\"", "🦀x", 0..4, 1..11, 11..12),
        ] {
            let source = Snapshot::new("workflow.yml".into(), raw.into());
            let span = source.span(0..raw.len()).unwrap();
            let value = decode_scalar(span, 0).unwrap();
            assert_eq!(value.text(), expected);
            assert_eq!(
                value.origins(selected.clone()).unwrap(),
                vec![Origin::Transformed(source.span(token).unwrap())]
            );
            assert_eq!(
                value.origins(selected.end..expected.len()).unwrap(),
                vec![Origin::Exact(source.span(following).unwrap())]
            );
        }
    }

    #[test]
    fn folded_text_maps_words_exactly_and_the_fold_as_a_transformation() {
        let raw = ">-\n  first\n  second\n";
        let source = Snapshot::new("workflow.yml".into(), raw.into());
        let span = source.span(0..raw.len()).unwrap();
        let value = decode_scalar(span.clone(), 0).unwrap();
        assert_eq!(value.text(), "first second");
        assert_eq!(
            value.origins(0..5).unwrap(),
            vec![Origin::Exact(source.span(5..10).unwrap())]
        );
        assert_eq!(
            value.origins(5..6).unwrap(),
            vec![Origin::Transformed(source.span(10..13).unwrap())]
        );
        assert_eq!(
            value.origins(6..12).unwrap(),
            vec![Origin::Exact(source.span(13..19).unwrap())]
        );
    }

    #[test]
    fn block_mapping_preserves_repeated_lines_and_unicode_across_line_endings() {
        for newline in ["\n", "\r\n", "\r"] {
            for (style, expected) in [
                ("|-", "🦀same\n🦀same"),
                (">-", "🦀same 🦀same"),
                ("|", "🦀same\n🦀same\n"),
                (">+", "🦀same 🦀same\n"),
            ] {
                let raw = format!("{style}{newline}  🦀same{newline}  🦀same{newline}");
                let source = Snapshot::new("workflow.yml".into(), raw.clone());
                let value = decode_scalar(source.span(0..raw.len()).unwrap(), 0).unwrap();
                assert_eq!(value.text(), expected);
                let first = style.len() + newline.len() + 2;
                let second = first + "🦀same".len() + newline.len() + 2;
                assert_eq!(
                    value.origins(0..8).unwrap(),
                    vec![Origin::Exact(source.span(first..first + 8).unwrap())]
                );
                assert_eq!(
                    value.origins(9..17).unwrap(),
                    vec![Origin::Exact(source.span(second..second + 8).unwrap())]
                );
            }
        }
    }

    #[test]
    fn blank_lines_extra_indent_and_chomping_preserve_text_origins() {
        let raw = ">2+\n\n    first\n      nested\n\n    last\n\n";
        let source = Snapshot::new("workflow.yml".into(), raw.into());
        let value = decode_scalar(source.span(0..raw.len()).unwrap(), 2).unwrap();
        assert_eq!(value.text(), "\nfirst\n  nested\n\nlast\n\n");
        for word in ["first", "  nested", "last"] {
            let decoded = value.text().find(word).unwrap();
            let original = raw.find(word).unwrap();
            assert_eq!(
                value.origins(decoded..decoded + word.len()).unwrap(),
                vec![Origin::Exact(
                    source.span(original..original + word.len()).unwrap()
                )]
            );
        }
        let source = Snapshot::new("workflow.yml".into(), "|\n  text".into());
        let value = decode_scalar(source.span(0..source.text().len()).unwrap(), 0).unwrap();
        assert_eq!(value.text(), "text\n");
        assert_eq!(value.origins(4..5).unwrap(), vec![Origin::Generated]);
    }
}
