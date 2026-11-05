# PSScriptAnalyzer DiagnosticRecord[] -> actionlint snippet diagnostics.
# Severity is the DiagnosticSeverity enum's numeric JSON representation.
def severity:
  if . == 0 then "info"
  elif . == 1 then "warning"
  elif . == 2 or . == 3 then "error"
  else error("Unknown PSScriptAnalyzer severity")
  end;

map({
  filename: $vars.filename,
  code: .RuleName,
  message: .Message,
  severity: (.Severity | severity),
  location: {row: .Extent.StartLineNumber, column: .Extent.StartColumnNumber},
  end_location: {row: .Extent.EndLineNumber, column: .Extent.EndColumnNumber},
  fixes: [(.SuggestedCorrections // [])[] | {
    description: .Description,
    replacements: [{
      row: .StartLineNumber,
      column: .StartColumnNumber,
      end_row: .EndLineNumber,
      end_column: .EndColumnNumber,
      text: .Text
    }]
  }]
})
