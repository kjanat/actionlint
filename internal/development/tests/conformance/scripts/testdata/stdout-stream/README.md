# Stdout stream and termination reference

`reference.jsonl` records 18 independently executed Bash scripts. Each entry
contains the complete stdout, process exit code and expected workflow-command
finding count. `environment.txt` records the Bash version and host platform.
The scripts run with the explicit runner Bash flags; the custom `bash +e {0}`
case instead starts with errexit disabled.

The corpus covers annotation and stop/resume tokens split across writes,
carriage-return/newline boundaries, final partial lines, termination under
errexit, literal option changes, ignored errexit contexts, subshells and captured
command substitutions. These cases came from the final independent review of
actionlint PR343 and its valid counterexamples.

The recorded test compares complete modeled lines against this capture. To
repeat every execution from the repository root in PowerShell:

```powershell
$env:ACTIONLINT_BASH_REFERENCE = 'C:/Program Files/Git/bin/bash.exe'
mise exec -- go test ./internal/development/tests/conformance/scripts -run '^TestStdoutStreamReference' -count=1
```

Set the variable to the Bash executable on the current host. Without it, normal
tests replay the recorded capture and skip live Bash execution. No GitHub runner
process is executed by this test; runner command parsing has separate pinned
reference evidence in the parent directory.
