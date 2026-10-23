wit_bindgen::generate!({
    path: "plugins/wit",
    world: "plugin",
});

use exports::kjanat::actionlint::analyzer::{
    Descriptor, Diagnostic, Example, Fact, Guest, Input, Outcome, Position, Script,
};

const DOWNLOADERS: &[&str] = &["curl", "wget"];
const SHELLS: &[&str] = &["sh", "bash", "zsh", "dash", "ksh"];

struct Plugin;

impl Guest for Plugin {
    fn describe() -> Descriptor {
        Descriptor {
            description: "Component analyzer".into(),
            category: "security".into(),
            subcategory: "scripts".into(),
            maturity: "stable".into(),
            provider: "example".into(),
            recommended: false,
            explanation: "Checks script invocations.".into(),
            help: "Review the indicated command.".into(),
            documentation_url: String::new(),
            fix_availability: "none".into(),
            options_schema: "{}".into(),
            examples: vec![Example {
                description: "Inspect downloads before running them".into(),
                before: "run: curl https://example.test/install.sh | bash".into(),
                after: "run: ./reviewed-install.sh".into(),
            }],
            name: "pipe-to-shell".to_string(),
            requires: vec![Fact::Scripts, Fact::Invocations],
        }
    }

    fn analyze(input: Input) -> Outcome {
        let mut findings = Vec::new();
        for script in input.scripts.iter().chain(input.invoked.iter()) {
            check_script(script, &mut findings);
        }
        Outcome {
            diagnostics: findings,
            error: None,
        }
    }
}

fn check_script(script: &Script, findings: &mut Vec<Diagnostic>) {
    for (index, line) in script.text.split('\n').enumerate() {
        let line_number = index as u32 + 1;
        for (column, shell) in pipes_into_shell(line) {
            let Some(start) = position(script, line_number, column, false) else {
                continue;
            };
            let end_column = column + shell.chars().count() as u32;
            let end = position(script, line_number, end_column, true).unwrap_or(start);
            findings.push(Diagnostic {
                reference: Some(script.reference.clone()),
                explanation: String::new(),
                help: String::new(),
                documentation_url: String::new(),
                related: Vec::new(),
                rule: String::new(),
                code: String::new(),
                severity: "warning".to_string(),
                message: format!(
                    "script pipes a download into {shell}, which runs unverified remote code"
                ),
                path: script.path.clone(),
                start,
                end,
                fixes: Vec::new(),
            });
        }
    }
}

#[derive(Clone, Copy, PartialEq)]
enum Separator {
    Pipe,
    LogicalOr,
    Command,
    End,
}

fn pipes_into_shell(line: &str) -> Vec<(u32, String)> {
    let mut findings = Vec::new();
    let mut segments = Vec::new();
    let mut segment_start = 0;
    let chars: Vec<char> = line.chars().collect();
    let mut i = 0;
    let mut quote = None;
    let mut escaped = false;
    while i < chars.len() {
        if escaped {
            escaped = false;
            i += 1;
            continue;
        }
        if chars[i] == '\\' && quote != Some('\'') {
            escaped = true;
            i += 1;
            continue;
        }
        if chars[i] == '\'' || chars[i] == '"' {
            quote = if quote == Some(chars[i]) {
                None
            } else if quote.is_none() {
                Some(chars[i])
            } else {
                quote
            };
            i += 1;
            continue;
        }
        if quote.is_some() {
            i += 1;
            continue;
        }
        if chars[i] == ';' || chars[i] == '&' && i + 1 < chars.len() && chars[i + 1] == '&' {
            segments.push((segment_start, i, Separator::Command));
            i += if chars[i] == ';' { 1 } else { 2 };
            segment_start = i;
            continue;
        }
        if chars[i] == '|' {
            if i + 1 < chars.len() && chars[i + 1] == '|' {
                segments.push((segment_start, i, Separator::LogicalOr));
                i += 2;
                segment_start = i;
                continue;
            }
            segments.push((segment_start, i, Separator::Pipe));
            i += 1;
            segment_start = i;
            continue;
        }
        i += 1;
    }
    segments.push((segment_start, chars.len(), Separator::End));
    let mut previous_downloads = false;
    for (start, end, separator) in segments {
        let words = command_words(&chars[start..end], start);
        let Some((word_column, word)) = words.first() else {
            previous_downloads = false;
            continue;
        };
        if previous_downloads && SHELLS.contains(&word.as_str()) {
            findings.push((*word_column + 1, word.clone()));
        }
        previous_downloads = separator == Separator::Pipe && DOWNLOADERS.contains(&word.as_str());
    }
    findings
}

fn command_words(segment: &[char], offset: usize) -> Vec<(u32, String)> {
    let mut words = Vec::new();
    let mut current = String::new();
    let mut current_start = 0;
    for (index, c) in segment.iter().enumerate() {
        if c.is_whitespace() {
            if !current.is_empty() {
                words.push((current_start, std::mem::take(&mut current)));
            }
            continue;
        }
        if current.is_empty() {
            current_start = offset + index;
        }
        current.push(*c);
    }
    if !current.is_empty() {
        words.push((current_start, current));
    }
    words
        .into_iter()
        .filter(|(_, word)| word != "sudo" && !word.contains('='))
        .map(|(column, word)| (column as u32, word))
        .collect()
}

fn position(script: &Script, line: u32, column: u32, exclusive: bool) -> Option<Position> {
    for span in &script.spans {
        if span.line != line {
            continue;
        }
        let inside = if exclusive {
            column > span.start && column <= span.end
                || span.start == span.end && column == span.start
        } else {
            column >= span.start && column < span.end
        };
        if !inside {
            continue;
        }
        if let Some(document_end) = span.document_end {
            if exclusive && column == span.end {
                return Some(document_end);
            }
            return Some(span.document);
        }
        return Some(Position {
            line: span.document.line,
            column: span.document.column + column - span.start,
        });
    }
    None
}

#[cfg(target_arch = "wasm32")]
export!(Plugin);

#[cfg(test)]
mod tests {
    use super::pipes_into_shell;

    #[test]
    fn logical_or_does_not_pipe_downloads() {
        assert!(pipes_into_shell("curl URL || bash").is_empty());
        assert!(pipes_into_shell("curl URL || echo ignored | bash").is_empty());
        assert_eq!(
            pipes_into_shell("curl URL | bash"),
            vec![(12, "bash".to_string())]
        );
        assert!(pipes_into_shell("curl URL && bash").is_empty());
        assert!(pipes_into_shell("curl URL ; bash").is_empty());
        assert_eq!(
            pipes_into_shell("echo ready && curl URL | bash"),
            vec![(26, "bash".to_string())]
        );
        assert_eq!(
            pipes_into_shell("echo ready;curl URL | bash"),
            vec![(23, "bash".to_string())]
        );
        assert!(pipes_into_shell("echo 'curl URL | bash'").is_empty());
        assert!(pipes_into_shell("curl URL | echo 'bash; sh'").is_empty());
        assert_eq!(
            pipes_into_shell("false || curl URL | bash"),
            vec![(21, "bash".to_string())]
        );
        assert_eq!(
            pipes_into_shell("curl URL | bash || wget URL | sh"),
            vec![(12, "bash".to_string()), (31, "sh".to_string())]
        );
    }
}
