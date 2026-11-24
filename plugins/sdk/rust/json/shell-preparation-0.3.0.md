# Shell preparation 0.3.0

`kjanat:actionlint-script/shell-script-inputs@0.3.0` supplies shell source, interpreter facts and source locations for a run occurrence in the current analysis. Native process manifests select `kjanat:actionlint/shell-preparation@0.3.0`.

The host masks workflow expressions, includes attached YAML comments and inserts startup commands after leading comments. The line map marks each line as original script, attached comment or generated text. An invalid interpreter invocation returns no prepared script.

The generated startup line ends with an `export` of the declared environment names available in every OS alternative, with their spelling. It holds only ASCII shell identifiers and no values.

Environment facts cover declarations and predefined runner variables. They omit ambient runner variables and `GITHUB_ENV` changes. The plugin decides which checks run when names or their casing are unknown.

The optional `kjanat:actionlint-script/environments@0.3.0` interface exposes these facts. `for-script` accepts only script occurrences supplied to the document check. `current` serves only the active snippet-engine check. Other queries return an error. An empty list means the host has no facts for the script. A populated result lists OS alternatives, both name comparisons, declaration scopes and unresolved-name status, without values.
