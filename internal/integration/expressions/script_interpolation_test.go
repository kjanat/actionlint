package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestScriptInterpolationWorkflowProofs(t *testing.T) {
	for _, tc := range []struct {
		name, matrix, env, expression, want string
	}{
		{name: "unknown input", expression: "inputs.text", want: "script-interpolation"},
		{name: "secret", expression: "secrets.TOKEN", want: "script-injection"},
		{name: "boolean", expression: "inputs.flag"},
		{name: "boolean branches", expression: "inputs.flag && 'release' || 'debug'"},
		{name: "safe matrix", matrix: "target: [x86_64-linux, aarch64-linux]", expression: "matrix.target"},
		{name: "unsafe matrix alternative", matrix: `target: [safe, "bad'quote"]`, expression: "matrix.target", want: "script-interpolation"},
		{name: "unsafe include", matrix: "target: [safe]\n        include: [{target: \"bad'quote\"}]", expression: "matrix.target", want: "script-interpolation"},
		{name: "unknown include", matrix: "target: [safe]\n        include: ${{ fromJSON(inputs.text) }}", expression: "matrix.target", want: "script-interpolation"},
		{name: "known step env", env: "VALUE: safe", expression: "env.VALUE"},
		{name: "unsafe step env", env: `VALUE: "bad'quote"`, expression: "env.VALUE", want: "script-interpolation"},
		{name: "unknown step env", env: "VALUE: ${{ inputs.text }}", expression: "env.VALUE", want: "script-interpolation"},
		{name: "known url", expression: "github.repositoryUrl"},
		{name: "unknown ref", expression: "github.ref_name", want: "script-interpolation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := "on: {workflow_dispatch: {inputs: {text: {type: string}, flag: {type: boolean}}}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n"
			if tc.matrix != "" {
				content += "    strategy:\n      matrix:\n        " + tc.matrix + "\n"
			}
			content += "    steps:\n      - run: |\n          printf '%s\\n' '${{ " + tc.expression + " }}'\n"
			if tc.env != "" {
				content += "        env:\n          " + tc.env + "\n"
			}
			cfg, err := repoconfig.Parse([]byte("tools: {shellcheck: false, ruff: false}"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yml", Content: []byte(content), Config: cfg}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatal(result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != tc.want || !strings.Contains(result.Diagnostics[0].Message, "environment variable") {
				t.Fatalf("want one %s; got %v", tc.want, result.Diagnostics)
			}
		})
	}
}

func TestTemplateStringNeedsShellWordProof(t *testing.T) {
	for _, tc := range []struct {
		name, expression, env, matrix, command string
		wordSafe                               bool
	}{
		{name: "numeric literal", expression: `'2'`},
		{name: "numeric github run id", expression: `github.run_id`},
		{name: "numeric github run number", expression: `github.run_number`},
		{name: "numeric github run attempt", expression: `github.run_attempt`},
		{name: "numeric github repository id", expression: `github.repository_id`},
		{name: "numeric github repository owner id", expression: `github.repository_owner_id`},
		{name: "numeric github actor id", expression: `github.actor_id`},
		{name: "numeric github sha", expression: `github.sha`},
		{name: "possibly empty hashFiles result", expression: `hashFiles('missing-*')`, command: `if true; then ${{ hashFiles('missing-*') }}; fi`},
		{name: "numeric environment value", expression: `env.FD`, env: `FD: "2"`},
		{name: "numeric matrix value", expression: `matrix.fd`, matrix: `fd: ["2"]`},
		{name: "empty literal", expression: `''`},
		{name: "empty environment value", expression: `env.WORD`, env: `WORD: ""`},
		{name: "empty matrix value", expression: `matrix.word`, matrix: `word: [""]`},
		{name: "optional matrix object member", expression: `matrix.item.word`, matrix: `item: [{word: echo}, {}]`},
		{name: "optional include-only object member", expression: `matrix.item.word`, matrix: `include: [{item: {word: echo}}, {item: {}}]`},
		{name: "omitted include-only matrix value", expression: `matrix.word`, matrix: `include: [{word: echo}, {other: safe}]`},
		{name: "included matrix value across base rows", expression: `matrix.word`, matrix: `target: [linux, windows], include: [{word: echo}]`, wordSafe: true},
		{name: "filtered include-only matrix value with base rows", expression: `matrix.word`, matrix: `target: [linux, windows], include: [{target: linux, word: echo}]`},
		{name: "standalone include omits base axis", expression: `matrix.os`, matrix: `os: [echo], version: [1], include: [{version: 2}]`, command: `if true; then ${{ matrix.os }}; fi`},
		{name: "standalone include omits nested base member", expression: `matrix.item.word`, matrix: `item: [{word: echo}], version: [1], include: [{version: 2}]`},
		{name: "standalone include supplies base axis", expression: `matrix.os`, matrix: `os: [echo], version: [1], include: [{os: printf, version: 2}]`, command: `if true; then ${{ matrix.os }}; fi`, wordSafe: true},
		{name: "matching include inherits omitted axis", expression: `matrix.os`, matrix: `os: [echo], version: [1], include: [{version: 1}]`, command: `if true; then ${{ matrix.os }}; fi`, wordSafe: true},
		{name: "matching include inherits multiple rows", expression: `matrix.os`, matrix: `os: [echo, printf], version: [1, 2], include: [{version: 2}]`, command: `if true; then ${{ matrix.os }}; fi`, wordSafe: true},
		{name: "earlier include cannot fill standalone job", expression: `matrix.os`, matrix: `os: [echo], version: [1], include: [{os: printf, version: 2}, {version: 2}]`, command: `if true; then ${{ matrix.os }}; fi`},
		{name: "include readds excluded sparse job", expression: `matrix.os`, matrix: `os: [echo], version: [1], exclude: [{version: 1}], include: [{version: 1}]`, command: `if true; then ${{ matrix.os }}; fi`},
		{name: "include readds complete excluded job", expression: `matrix.os`, matrix: `os: [echo], version: [1], exclude: [{version: 1}], include: [{os: echo, version: 1}]`, command: `if true; then ${{ matrix.os }}; fi`, wordSafe: true},
		{name: "exclusion without includes preserves base proof", expression: `matrix.os`, matrix: `os: [echo], version: [1, 2], exclude: [{version: 1}]`, command: `if true; then ${{ matrix.os }}; fi`, wordSafe: true},
		{name: "matching include survives unrelated exclusion", expression: `matrix.os`, matrix: `os: [echo], version: [1, 2], exclude: [{version: 2}], include: [{version: 1}]`, command: `if true; then ${{ matrix.os }}; fi`, wordSafe: true},
		{name: "ragged matrix array index", expression: `matrix.target[1]`, matrix: `target: [[linux], [linux, windows]]`},
		{name: "missing matrix object key", expression: `matrix.item[matrix.key]`, matrix: `item: [{name: linux}], key: [missing]`},
		{name: "empty matrix object wildcard", expression: `matrix.item[matrix.key]`, matrix: `item: [{}], key: [missing]`},
		{name: "empty matrix array wildcard", expression: `matrix.target[matrix.index]`, matrix: `target: [[]], index: [0]`},
		{name: "dynamic matrix index", expression: `matrix.target[matrix.index]`, matrix: `target: [[linux]], index: [0]`},
		{name: "definite nested object member", expression: `matrix.item.word`, matrix: `item: [{word: echo}, {word: printf}]`, wordSafe: true},
		{name: "definite nested array index", expression: `matrix.target[1]`, matrix: `target: [[linux, windows], [linux, macos]]`, wordSafe: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow := func(command string) string {
				source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n"
				if tc.matrix != "" {
					source += "    strategy:\n      matrix: {" + tc.matrix + "}\n"
				}
				source += "    steps:\n      - "
				if tc.env != "" {
					source += "env: {" + tc.env + "}\n        "
				}
				return source + "run: " + command + "\n"
			}
			cfg, err := repoconfig.Parse([]byte("tools: {shellcheck: false, ruff: false}"))
			if err != nil {
				t.Fatal(err)
			}
			unsafeCommand := tc.command
			if unsafeCommand == "" {
				unsafeCommand = "if true; then echo ${{ " + tc.expression + "}}>out; fi"
			}
			wantCommand := "script-interpolation"
			if tc.wordSafe {
				wantCommand = ""
			}
			for _, check := range []struct {
				name, command, want string
			}{
				{"command syntax", unsafeCommand, wantCommand},
				{"data argument", "printf '%s\\n' \"${{ " + tc.expression + " }}\"", ""},
			} {
				t.Run(check.name, func(t *testing.T) {
					result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yml", Content: []byte(workflow(check.command)), Config: cfg}})
					if err != nil {
						t.Fatal(err)
					}
					if check.want == "" {
						if len(result.Diagnostics) != 0 {
							t.Fatalf("data argument rejected: %+v", result.Diagnostics)
						}
					} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != check.want {
						t.Fatalf("got %+v, want only %s", result.Diagnostics, check.want)
					}
				})
			}
		})
	}
}

func TestMutableInheritedEnvIsNotScriptWordProof(t *testing.T) {
	for _, tc := range []struct {
		name, workflowEnv, jobEnv, stepEnv string
		wantFinding                        bool
	}{
		{name: "workflow env can change", workflowEnv: "env: {WORD: echo}", wantFinding: true},
		{name: "job env can change", jobEnv: "env: {WORD: echo}", wantFinding: true},
		{name: "step env overrides inherited value", workflowEnv: "env: {WORD: echo}", stepEnv: "env: {WORD: echo}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\n" + tc.workflowEnv + "\njobs:\n  test:\n    runs-on: ubuntu-latest\n    " + tc.jobEnv + "\n    steps:\n      - run: echo 'WORD=fi' >> \"$GITHUB_ENV\"\n      - " + tc.stepEnv + "\n        run: if true; then ${{ env.WORD }}; fi\n"
			cfg, err := repoconfig.Parse([]byte("tools: {shellcheck: false, ruff: false}"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yml", Content: []byte(source), Config: cfg}})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, diagnostic := range result.Diagnostics {
				found = found || diagnostic.Rule == "script-interpolation"
			}
			if found != tc.wantFinding {
				t.Fatalf("script-interpolation=%t, want %t: %+v", found, tc.wantFinding, result.Diagnostics)
			}
		})
	}
}

func TestLogicalScriptWordProofs(t *testing.T) {
	for _, tc := range []struct {
		name, expression string
		wordFinding      bool
		argumentFinding  bool
	}{
		{name: "unknown falsy and branch", expression: "inputs.command && 'echo'", wordFinding: true},
		{name: "unknown value in data branch", expression: "inputs.command && inputs.value", wordFinding: true, argumentFinding: true},
		{name: "numeric and branch", expression: "github.run_id && 'echo'", wordFinding: true},
		{name: "known true and branch", expression: "true && 'echo'"},
		{name: "known false and branch", expression: "false && 'echo'", wordFinding: true},
		{name: "false or branch", expression: "false || 'echo'"},
		{name: "true or branch returns boolean", expression: "true || 'echo'", wordFinding: true},
		{name: "unknown and inside ternary", expression: "inputs.flag && 'release' || 'debug'"},
		{name: "unknown and branch with fallback", expression: "inputs.command && 'echo' || 'printf'"},
		{name: "unknown or branch", expression: "inputs.command || 'echo'", wordFinding: true, argumentFinding: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := repoconfig.Parse([]byte("tools: {shellcheck: false, ruff: false}"))
			if err != nil {
				t.Fatal(err)
			}
			for _, check := range []struct {
				name, run string
				want      bool
			}{
				{name: "command word", run: "if true; then ${{ " + tc.expression + " }}; fi", want: tc.wordFinding},
				{name: "data argument", run: "printf '%s\\n' \"${{ " + tc.expression + " }}\"", want: tc.argumentFinding},
			} {
				t.Run(check.name, func(t *testing.T) {
					source := "on: {workflow_dispatch: {inputs: {command: {type: string}, value: {type: string}, flag: {type: boolean}}}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + check.run + "\n"
					result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yml", Content: []byte(source), Config: cfg}})
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, diagnostic := range result.Diagnostics {
						found = found || diagnostic.Rule == "script-interpolation"
					}
					if found != check.want {
						t.Fatalf("script-interpolation=%t, want %t: %+v", found, check.want, result.Diagnostics)
					}
				})
			}
		})
	}
}
