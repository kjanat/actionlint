use actionlint_model::source::{MappedText, Span};

struct Line {
    start: usize,
    end: usize,
    next: usize,
}

fn lines(raw: &str) -> Vec<Line> {
    let mut result = Vec::new();
    let mut start = 0;
    while start < raw.len() {
        let end = raw[start..]
            .find(['\r', '\n'])
            .map_or(raw.len(), |offset| start + offset);
        let next = if raw[end..].starts_with("\r\n") {
            end + 2
        } else if end < raw.len() {
            end + 1
        } else {
            end
        };
        result.push(Line { start, end, next });
        start = next;
    }
    result
}

pub(super) fn mapping(source: &Span, decoded: &str, parent_indent: usize) -> Option<MappedText> {
    let raw = source.text();
    let lines = lines(raw);
    let (header, body) = lines.split_first()?;
    let indicator = raw[..header.end]
        .split('#')
        .next()?
        .bytes()
        .skip(1)
        .find(u8::is_ascii_digit);
    let indent = match indicator {
        Some(digit) => parent_indent.checked_add(usize::from(digit - b'0'))?,
        None => body.iter().find_map(|line| {
            let content = &raw[line.start..line.end];
            let spaces = content.bytes().take_while(|byte| *byte == b' ').count();
            (spaces < content.len()).then_some(spaces)
        })?,
    };
    let base = source.bytes().start;
    let span = |start, end| source.source().span(base + start..base + end).ok();
    let mut result = MappedText::default();
    let mut previous = header.next;
    for line in body {
        let spaces = raw[line.start..line.end]
            .bytes()
            .take_while(|byte| *byte == b' ')
            .count();
        let start = line.start + spaces.min(indent);
        if start == line.end {
            continue;
        }
        let content = &raw[start..line.end];
        let tail = decoded.get(result.text().len()..)?;
        let offset = unique_prefix(tail, content)?;
        if offset > 0 {
            result.transform(&tail[..offset], span(previous, start)?);
        }
        result.copy(span(start, line.end)?);
        previous = line.end;
    }
    let tail = decoded.get(result.text().len()..)?;
    if !tail
        .chars()
        .all(|character| matches!(character, ' ' | '\n'))
    {
        return None;
    }
    if !tail.is_empty() {
        if previous == raw.len() {
            result.generate(tail);
        } else {
            result.transform(tail, span(previous, raw.len())?);
        }
    }
    (result.text() == decoded).then_some(result)
}

// Only whitespace produced by folding may separate consecutive source lines.
fn unique_prefix(decoded: &str, content: &str) -> Option<usize> {
    let mut selected = None;
    for (offset, character) in decoded.char_indices() {
        if decoded[offset..].starts_with(content) {
            if selected.is_some() {
                return None;
            }
            selected = Some(offset);
        }
        if !matches!(character, ' ' | '\n') {
            break;
        }
    }
    selected
}
