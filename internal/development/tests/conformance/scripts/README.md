# Script reference captures

These tests replay independently captured reference output. They do not run Bash or the GitHub runner during a normal Go test. The reference harnesses and captures are checked in so the cases can be repeated without the review artifacts.

`bash-reference.bash` runs each script with `bash --noprofile --norc -e -o pipefail -c`, matching the explicit Bash options in [ScriptHandlerHelpers.cs at runner commit 1ba885c2065fd9be317808ab3fd406f392635cf7](https://github.com/actions/runner/blob/1ba885c2065fd9be317808ab3fd406f392635cf7/src/Runner.Worker/Handlers/ScriptHandlerHelpers.cs). `bash-reference.txt` is the captured stdout and exit status for each case. The original capture did not record the Bash version or platform. The tests compare the captured execution with the model's finding about a later status read; they do not claim to simulate every Bash execution path.

`runner-reference.jsonl` records output from the original `Program.cs` driver compiled with .NET 9 and [ActionCommand.cs at the same runner commit](https://github.com/actions/runner/blob/1ba885c2065fd9be317808ab3fd406f392635cf7/src/Runner.Common/ActionCommand.cs). The driver calls `ActionCommand.TryParseV2` with an explicit set of registered command names. It does not run `ActionCommandManager`, extensions, server feature flags, or stop/resume handling. The runner's [ActionCommandManager.cs](https://github.com/actions/runner/blob/1ba885c2065fd9be317808ab3fd406f392635cf7/src/Runner.Worker/ActionCommandManager.cs) also calls the legacy parser and handles those additional conditions. The model reports only rejected text that resembles a workflow command, so not every runner rejection is a finding. The original driver produced no stderr. Its exact .NET SDK version was not recorded.

To repeat the Bash capture from this directory on a host with Bash installed:

```sh
bash testdata/bash-reference.bash > bash-current.txt
diff -u testdata/bash-reference.txt bash-current.txt
```

To repeat the parser capture in PowerShell with .NET 9 installed, use a temporary directory. This downloads the original source from the pinned runner commit; it does not vendor or modify it.

```powershell
$referenceDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory $referenceDir | Out-Null
Copy-Item testdata/runner-reference/Program.cs, testdata/runner-reference/runner-reference.csproj $referenceDir
Invoke-WebRequest 'https://raw.githubusercontent.com/actions/runner/1ba885c2065fd9be317808ab3fd406f392635cf7/src/Runner.Common/ActionCommand.cs' -OutFile (Join-Path $referenceDir 'ActionCommand.cs')
dotnet run --project (Join-Path $referenceDir 'runner-reference.csproj') > runner-current.jsonl
```

Compare `runner-current.jsonl` with `testdata/runner-reference.jsonl`. .NET build messages may need to be excluded from stdout before comparison. Remove the temporary directory and output files after inspection.
