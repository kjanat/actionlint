# Shell preparation 0.3.0

`kjanat:actionlint-script/shell-script-inputs@0.3.0` implements the
[0.2.0 preparation steps](shell-preparation-0.2.0.md), with the following changes.
Native process manifests select `kjanat:actionlint/shell-preparation@0.3.0`.

Preparation appends an `export` command to the generated startup line. It lists
declared environment names available across every OS alternative, preserving
their spelling. It assigns no values and includes no secrets. Only ASCII shell
identifiers are emitted, so declaration keys cannot insert commands. The line
map identifies this text as generated startup rather than workflow source.

Runner merging and shell lookup are separate operations. Windows merges keys
without regard to case and preserves the first spelling. Bash looks up that
spelling exactly. PowerShell environment lookup ignores case on Windows and
distinguishes case on Linux and macOS.

Default exclusions are SC1091, SC2194, SC2050, SC2157 and SC2043. SC2153 and SC2154
are excluded for an individual script only when declaration names or their
exported spelling cannot be resolved, or environment facts were not supplied.
The names describe declarations and predefined runner variables. They do not
enumerate ambient runner variables or changes made through `GITHUB_ENV`.

The optional `kjanat:actionlint-script/environments@0.3.0` interface exposes these facts.
`for-script` accepts only script occurrences supplied to the document check.
`current` serves only the active snippet-engine check. Queries outside those
scopes return an error. Each result includes OS alternatives, both name
comparisons, declaration scopes and unresolved-name status, without values.
