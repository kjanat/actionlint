package scripts

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/interpreters"
	"actionlint.kjanat.dev/v2/internal/model/commands"
)

func TestLiteralStdoutWorkflowCommands(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, language, text string
		want                 int
	}{
		{"bash", "bash", `echo "::notice:Up to date"`, 1},
		{"posix", "sh", `echo '::error:msg'`, 1},
		{"python", "python", `print("::warning single-colon")`, 1},
		{"powershell", "pwsh", `Write-Host '::group unterminated'`, 1},
		{"powershell echo", "powershell", `echo '::notic::msg'`, 1},
		{"printf", "bash", `printf '%s\n' '::error:msg'`, 1},
		{"unused", "bash", `EXAMPLE='::notic::msg'`, 0},
		{"comment", "bash", `# echo '::notic::msg'`, 0},
		{"quoted example", "python", `'print("::notic::msg")'`, 0},
		{"python unused", "python", `example = '::notic::msg'`, 0},
		{"powershell unused", "pwsh", `$example = '::notic::msg'`, 0},
		{"powershell type", "pwsh", `[System.IO.Path]::GetTempPath()`, 0},
		{"redirected", "bash", `echo '::notic::msg' > example.txt`, 0},
		{"function body", "bash", `f() { echo '::notic::msg'; }`, 0},
		{"dynamic stop", "bash", "echo \"::stop-commands::$token\"\necho '::notic::msg'", 0},
		{"dynamic output", "bash", "echo \"$text\"\necho '::notic::msg'", 0},
		{"stop resume", "bash", "echo '::stop-commands::token'\necho '::notic::example'\necho '::token::'\necho '::notice:broken'", 1},
		{"multiline python", "python", "text = '''\nprint('::notic::example')\n'''", 0},
		{"multiline powershell", "pwsh", "$text = @'\nWrite-Host '::notic::example'\n'@", 0},
		{"unknown interpreter", "unknown", `echo '::notic::msg'`, 0},
		{"python trailing parse error", "python", "print('::notic::msg')\nnot valid ???", 0},
		{"powershell trailing parse error", "pwsh", "Write-Host '::notic::msg'\n}", 0},
		{"else branch", "bash", `x="$(git diff)"; if [[ "$x" != '' ]]; then echo '::error::changed'; else echo '::notice:up to date'; fi`, 1},
		{"unreachable then", "bash", `if false; then echo '::notic::example'; else :; fi`, 0},
		{"unreachable else", "bash", `if true; then :; else echo '::notic::example'; fi`, 0},
		{"branch stop token", "bash", `if test -f x; then echo '::stop-commands::token'; else echo '::notic::msg'; fi`, 1},
		{"branch resume distinct", "bash", `if test -f x; then echo '::stop-commands::token'; else :; fi; echo '::notic::msg'`, 1},
		{"all branches stopped", "bash", `if test -f x; then echo '::stop-commands::token'; else echo '::stop-commands::other'; fi; echo '::notic::example'`, 0},
		{"joined annotation", "bash", `printf '::notice'; echo '::ok'`, 0},
		{"joined stop token", "bash", `printf '::stop-commands::'; echo token; echo '::notice: bad'`, 0},
		{"joined resume token", "bash", `echo '::stop-commands::token'; printf '::to'; echo 'ken::'; echo '::notice:bad'`, 1},
		{"partial command known eof", "bash", `printf '::notice'`, 1},
		{"partial command unknown completion", "bash", `printf '::notice'; unknown-command`, 0},
		{"errexit termination", "bash", `false; echo '::notice: bad'`, 0},
		{"disabled errexit continues", "bash", `set +e; false; echo '::notice: bad'`, 1},
		{"restored errexit terminates", "bash", `set +e; set -e; false; echo '::notice: bad'`, 0},
		{"ignored group continues", "bash", `{ false; echo '::notice: bad'; } || :`, 1},
		{"negated group continues", "bash", `! { false; echo '::notice: bad'; }`, 1},
		{"condition list continues", "bash", `if false; echo '::notice: bad'; then :; fi`, 1},
		{"exit terminates", "bash", `exit 0; echo '::notice: bad'`, 0},
		{"partial command terminated", "bash", `printf '::notice'; false; echo '::ok'`, 1},
		{"subshell options isolated", "bash", `(set +e; false); false; echo '::notice: bad'`, 0},
		{"failed subshell terminates parent", "bash", `(set +e; false); echo '::notice: bad'`, 0},
		{"failed substitution terminates assignment", "bash", `x=$(false); echo '::notice: bad'`, 0},
		{"last substitution succeeds", "bash", `x=$(false) y=$(true); echo '::notice: bad'`, 1},
		{"substitution stdout stays captured", "bash", `x=$(echo '::notice: bad'); echo '::notice::ok'`, 0},
		{"explicit exit bypasses negation", "bash", `(! exit 1); echo '::notice: bad'`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			count := 0
			seen := make(map[ScriptRange]bool)
			invocation := interpreters.Builtin().Analyze(tc.language, false)
			if tc.language == "python" || tc.language == "pwsh" || tc.language == "powershell" {
				invocation = commands.Invocation{Status: commands.Valid, Language: tc.language, Input: "file"}
			}
			for _, path := range LiteralStdoutPaths(tc.text, invocation, interpreters.Builtin()) {
				var state commands.WorkflowCommandState
				for _, output := range path {
					if output.Start < 0 || output.End > len(tc.text) || output.Start >= output.End {
						t.Fatalf("bad source range: %+v", output)
					}
					if state.WorkflowCommandProblem(output.Text) != "" && !seen[output.ScriptRange] {
						count++
						seen[output.ScriptRange] = true
					}
				}
			}
			if count != tc.want {
				t.Fatalf("got %d, want %d", count, tc.want)
			}
		})
	}
}

func TestLiteralStdoutJoinedSourceRange(t *testing.T) {
	t.Parallel()
	const text = `printf '::not'; echo 'ic::bad'`
	registry := interpreters.Builtin()
	paths := LiteralStdoutPaths(text, registry.Analyze("bash", false), registry)
	if len(paths) != 1 || len(paths[0]) != 1 {
		t.Fatalf("unexpected stdout paths: %+v", paths)
	}
	line := paths[0][0]
	if line.Text != "::notic::bad" || line.Start != 7 || line.End != 14 || text[line.Start:line.End] != "'::not'" {
		t.Fatalf("joined line did not retain its first contributing range: %+v", line)
	}
}
