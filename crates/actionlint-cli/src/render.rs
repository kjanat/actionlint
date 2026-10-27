use actionlint_model::{
    analysis::{Diagnostic, Severity},
    source::Encoding,
};
use std::io::{self, Write};
use unicode_width::UnicodeWidthChar;

fn visible(text: &str) -> String {
    text.chars()
        .flat_map(|ch| {
            if ch.is_control() {
                ch.escape_default().collect::<Vec<_>>()
            } else {
                vec![ch]
            }
        })
        .collect()
}

pub fn diagnostic(out: &mut impl Write, diagnostic: &Diagnostic, color: bool) -> io::Result<()> {
    let (label, ansi) = match diagnostic.severity {
        Severity::Error => ("error", "31"),
        Severity::Warning => ("warning", "33"),
        Severity::Information => ("info", "36"),
        Severity::Hint => ("hint", "36"),
    };
    let heading = format!("{label}[{}]", visible(&diagnostic.rule));
    if color {
        write!(out, "\x1b[{ansi};1m{heading}\x1b[0m")?;
    } else {
        write!(out, "{heading}")?;
    }
    writeln!(out, ": {}", visible(&diagnostic.message))?;
    let source = diagnostic.primary.source();
    let bytes = diagnostic.primary.bytes();
    let position = source
        .position(bytes.start, Encoding::Utf8)
        .map_err(io::Error::other)?;
    let line = source.line(position.line).map_err(io::Error::other)?;
    let prefix = &line[..position.character];
    let line_start = bytes.start - position.character;
    let end = bytes.end.min(line_start + line.len()) - line_start;
    let selection = &line[position.character..end];
    let before: Vec<char> = visible(prefix).chars().collect();
    let skip = before.len().saturating_sub(35);
    let before: String = before[skip..].iter().collect();
    let after: String = visible(&line[position.character..])
        .chars()
        .take(80)
        .collect();
    let ellipsis = if skip > 0 { "…" } else { "" };
    let offset: usize = before
        .chars()
        .map(|ch| ch.width().unwrap_or(0))
        .sum::<usize>()
        + usize::from(skip > 0);
    let width = visible(selection)
        .chars()
        .take(80)
        .map(|ch| ch.width().unwrap_or(0))
        .sum::<usize>()
        .max(1);
    let column = source
        .position(bytes.start, Encoding::UnicodeScalar)
        .map_err(io::Error::other)?
        .character
        + 1;
    writeln!(
        out,
        "  --> {}:{}:{column}",
        visible(source.uri()),
        position.line + 1
    )?;
    writeln!(out, "   |")?;
    writeln!(out, "{:>3} | {ellipsis}{before}{after}", position.line + 1)?;
    writeln!(out, "   | {}{}", " ".repeat(offset), "^".repeat(width))?;
    if bytes.end > line_start + line.len() {
        writeln!(out, "   = range continues on following lines")?;
    }
    for related in &diagnostic.related {
        let location = related
            .location
            .source()
            .position(related.location.bytes().start, Encoding::UnicodeScalar)
            .map_err(io::Error::other)?;
        writeln!(
            out,
            "   = {}:{}:{}: {}",
            visible(related.location.source().uri()),
            location.line + 1,
            location.character + 1,
            visible(&related.message)
        )?;
    }
    for fix in &diagnostic.fixes {
        let policy = match fix.applicability {
            actionlint_model::analysis::Applicability::Automatic => "automatic",
            actionlint_model::analysis::Applicability::RequiresReview => "review required",
        };
        writeln!(
            out,
            "   = help: {} ({policy}; {} edits)",
            visible(&fix.title),
            fix.edits.len()
        )?;
    }
    writeln!(out)
}

#[cfg(test)]
mod tests {
    use super::*;
    use actionlint_model::source::Snapshot;

    #[test]
    fn escapes_terminal_controls_and_aligns_wide_characters() {
        let source = Snapshot::new("workflow.yml".into(), "界\tmissing\x1b[31m".into());
        let diagnostic = Diagnostic {
            related: Vec::new(),
            fixes: Vec::new(),
            rule: "workflow/test".into(),
            message: "missing job\x1b[2J".into(),
            severity: Severity::Error,
            primary: source.span(4..11).unwrap(),
        };
        let mut output = Vec::new();
        super::diagnostic(&mut output, &diagnostic, false).unwrap();
        let output = String::from_utf8(output).unwrap();
        assert!(!output.contains('\x1b'));
        assert!(output.contains("   |     ^^^^^^^"), "{output}");
        assert!(output.contains("workflow.yml:1:3"));
    }
}
