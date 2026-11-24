# Script reference captures

`bash-reference.bash` runs each script with the Bash options from [ScriptHandlerHelpers.cs](https://github.com/actions/runner/blob/1ba885c2065fd9be317808ab3fd406f392635cf7/src/Runner.Worker/Handlers/ScriptHandlerHelpers.cs). `bash-reference.txt` holds the stdout and exit status of each case.

`runner-reference.jsonl` holds the output of the `Program.cs` driver. The driver calls `ActionCommand.TryParseV2` from [ActionCommand.cs](https://github.com/actions/runner/blob/1ba885c2065fd9be317808ab3fd406f392635cf7/src/Runner.Common/ActionCommand.cs).

To repeat the Bash capture from this directory:

```sh
bash testdata/bash-reference.bash >bash-current.txt
diff -u testdata/bash-reference.txt bash-current.txt
```

To repeat the parser capture in PowerShell with .NET 9:

```powershell
$referenceDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory $referenceDir | Out-Null
Copy-Item testdata/runner-reference/Program.cs, testdata/runner-reference/runner-reference.csproj $referenceDir
Invoke-WebRequest 'https://raw.githubusercontent.com/actions/runner/1ba885c2065fd9be317808ab3fd406f392635cf7/src/Runner.Common/ActionCommand.cs' -OutFile (Join-Path $referenceDir 'ActionCommand.cs')
dotnet run --project (Join-Path $referenceDir 'runner-reference.csproj') > runner-current.jsonl
Remove-Item -Recurse $referenceDir
```

Remove .NET build messages from `runner-current.jsonl`, then compare it with `testdata/runner-reference.jsonl`.
