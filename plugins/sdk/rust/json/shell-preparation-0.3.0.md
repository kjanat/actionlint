# Shell preparation 0.3.0

`kjanat:actionlint-script/shell-script-inputs@0.3.0` supplies shell source,
interpreter facts and source locations for a run occurrence in the current analysis.
Native process manifests select `kjanat:actionlint/shell-preparation@0.3.0`.

The host masks workflow expressions, includes attached YAML comments and inserts
the interpreter's startup commands after leading comments. It maps each line to
the original script, an attached comment or generated text. A supplied dialect
override replaces the inferred dialect and drops incompatible startup commands.
An invalid interpreter invocation returns no prepared script. An unknown dialect
is passed to the plugin, which decides whether it supports the script.

The host passes comment text to the plugin without parsing tool directives.
The integration selects tool options and diagnostic exclusions. The default
builder returns an empty `excluded` list; a custom builder can supply diagnostic
numbers through that field.

Preparation appends an `export` command to the generated startup line. It lists
declared environment names available across every OS alternative, preserving
their spelling. It assigns no values and includes no secrets. Only ASCII shell
identifiers are emitted, so declaration keys cannot insert commands. The line
map identifies this text as generated startup rather than workflow source.

Runner merging and shell lookup are separate operations. Windows merges keys
without regard to case and preserves the first spelling. Bash looks up that
spelling exactly. PowerShell environment lookup ignores case on Windows and
distinguishes case on Linux and macOS.

Environment facts describe declarations and predefined runner variables. They do
not enumerate ambient runner variables or changes made through `GITHUB_ENV`.
A plugin decides which checks can run when names or their casing are unknown.

The optional `kjanat:actionlint-script/environments@0.3.0` interface exposes these facts.
`for-script` accepts only script occurrences supplied to the document check.
`current` serves only the active snippet-engine check. Queries outside those
scopes return an error. An empty list means that the host has no environment
facts for the supplied script. Each populated result includes OS alternatives, both name
comparisons, declaration scopes and unresolved-name status, without values.
