# ShellCheck `-f json1` output -> script diagnostics.
#
# Snippet findings report the file "-" (stdin). A finding in a file ShellCheck
# followed with -x reports that file, which becomes the finding's filename and
# source. Messages drop one trailing period; fix descriptions keep the
# untrimmed message. A comment without an end has no end location.
def in_snippet: .file == null or .file == "" or .file == "-";
def replacement: {row: .line, end_row: .endLine, column: .column, end_column: .endColumn, text: .replacement};
{
  diagnostics: [
    .comments[]
    | .message as $message
    | {
        filename: (if in_snippet then $vars.filename else .file end),
        code: "SC\(.code)",
        message: ($message | rtrimstr(".")),
        severity: .level,
        location: {row: .line, column: .column},
        fixes: [
          .fix
          | select(. != null and (.replacements | length) > 0)
          | {description: $message, replacements: [.replacements[] | replacement]}
        ]
      }
      + (if .endLine == null then {} else {end_location: {row: .endLine, column: .endColumn}} end)
      + (if in_snippet then {} else {source: .file} end)
  ],
  sources: (.sources // [])
}
