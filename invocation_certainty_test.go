package actionlint

import (
	"runtime"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func TestLiteralShellWordQuotes(t *testing.T) {
	for _, tc := range []struct {
		word, want string
		known      bool
	}{
		{`"./{good,bad}.sh"`, "./{good,bad}.sh", true},
		{`"./*.sh"`, "./*.sh", true},
		{`"./[ab]?.sh"`, "./[ab]?.sh", true},
		{`"~/.sh"`, "~/.sh", true},
		{`./"{good,bad}".sh`, "./{good,bad}.sh", true},
		{`./{good,bad}.sh`, "", false},
		{`./*.sh`, "", false},
		{`"./$FILE"`, "", false},
		{`"./$(echo file)"`, "", false},
		{`"./\\file"`, "", false},
	} {
		t.Run(tc.word, func(t *testing.T) {
			file, err := syntax.NewParser().Parse(strings.NewReader(tc.word), "")
			if err != nil {
				t.Fatal(err)
			}
			call, ok := file.Stmts[0].Cmd.(*syntax.CallExpr)
			if !ok {
				t.Fatalf("not a call: %T", file.Stmts[0].Cmd)
			}
			got, known := literalShellWord(call.Args[0].Parts)
			if got != tc.want || known != tc.known {
				t.Fatalf("got %q, %v; want %q, %v", got, known, tc.want, tc.known)
			}
		})
	}
}

func TestExecutableBitUnixColonPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix colon paths cannot be represented on the Windows host")
	}
	root, git := executableFixture(t)
	for _, name := range []string{"scripts/a:b.sh", "a:debug/bad.sh"} {
		writeShellcheckFixture(t, root, name, "#!/bin/sh\necho script\n")
		git("add", "--", name)
		git("update-index", "--chmod=-x", "--", name)
	}
	for _, tc := range []struct{ name, checkout, step, want string }{
		{"script", "", "run: ./scripts/a:b.sh", "scripts/a:b.sh"},
		{"working directory", "", "run: ./bad.sh\nworking-directory: a:debug", "a:debug/bad.sh"},
		{"cd", "", "run: cd a:debug && ./bad.sh", "a:debug/bad.sh"},
		{"checkout", "        with: {path: 'a:debug'}\n", "run: ./a:debug/bad.sh", "bad.sh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n" + tc.checkout +
				"      - " + strings.ReplaceAll(tc.step, "\n", "\n        ") + "\n"
			file := writeShellcheckFixture(t, root, ".github/workflows/colon.yml", workflow)
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{file}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var findings []Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "executable-bit" {
					findings = append(findings, diagnostic)
				}
			}
			if len(findings) != 1 || !strings.Contains(findings[0].Message, `script "`+tc.want+`"`) {
				t.Fatalf("wanted tracked script %q: %+v", tc.want, result.Diagnostics)
			}
		})
	}
}

func TestExecutableBitRuntimeInputs(t *testing.T) {
	root, _ := executableFixture(t)
	for _, tc := range []struct {
		name, workflow, job, checkout, step string
		want                                bool
	}{
		{"workflow loader", "env: {LD_PRELOAD: library.so}\n", "", "", "", false},
		{"job loader", "", "env: {LD_AUDIT: library.so}\n", "", "", false},
		{"step loader", "", "", "", "env: {LD_PRELOAD: library.so}\n", false},
		{"checkout loader", "", "", "env: {LD_PRELOAD: library.so}\n", "", false},
		{"workflow Node options", "env: {NODE_OPTIONS: '--require ./setup.cjs'}\n", "", "", "", false},
		{"job Node options", "", "env: {NODE_OPTIONS: '--require ./setup.cjs'}\n", "", "", false},
		{"checkout Node options", "", "", "env: {NODE_OPTIONS: '--require ./setup.cjs'}\n", "", false},
		{"empty checkout Node options", "", "", "env: {NODE_OPTIONS: ''}\n", "", true},
		{"workflow SSH command", "env: {GIT_SSH_COMMAND: wrapper}\n", "", "", "", false},
		{"job SSH command", "", "env: {GIT_SSH_COMMAND: wrapper}\n", "", "", false},
		{"checkout SSH command", "", "", "env: {GIT_SSH_COMMAND: wrapper}\n", "", false},
		{"checkout SSH executable", "", "", "env: {GIT_SSH: wrapper}\n", "", false},
		{"empty SSH command", "", "", "env: {GIT_SSH_COMMAND: ''}\n", "", true},
		{"empty SSH executable", "", "", "env: {GIT_SSH: ''}\n", "", true},
		{"run-only SSH command", "", "", "", "env: {GIT_SSH_COMMAND: wrapper}\n", true},
		{"workflow proxy command", "env: {GIT_PROXY_COMMAND: wrapper}\n", "", "", "", false},
		{"job proxy command", "", "env: {GIT_PROXY_COMMAND: wrapper}\n", "", "", false},
		{"checkout proxy command", "", "", "with: {submodules: true}\nenv: {GIT_PROXY_COMMAND: wrapper}\n", "", false},
		{"empty proxy command", "", "", "env: {GIT_PROXY_COMMAND: ''}\n", "", true},
		{"run-only proxy command", "", "", "", "env: {GIT_PROXY_COMMAND: wrapper}\n", true},
		{"run-only Node options", "", "", "", "env: {NODE_OPTIONS: '--require ./setup.cjs'}\n", true},
		{"macOS loader", "", "", "", "env: {DYLD_INSERT_LIBRARIES: library.dylib}\n", false},
		{"loader search path", "", "", "", "env: {LD_LIBRARY_PATH: libraries}\n", false},
		{"empty loader search path", "", "", "", "env: {LD_LIBRARY_PATH: ''}\n", false},
		{"empty preload", "", "", "", "env: {LD_PRELOAD: ''}\n", true},
		{"ordinary environment", "env: {APP_ENV: test}\n", "", "", "", true},
		{"literal startup key", "", "", "", "env: {\"${{ 'BASH_ENV' }}\": ./setup.sh}\n", false},
		{"workflow literal startup key", "env: {\"${{ 'BASH_ENV' }}\": ./setup.sh}\n", "", "", "", false},
		{"job literal startup key", "", "env: {\"${{ 'ENV' }}\": ./setup.sh}\n", "", "", false},
		{"literal loader key", "", "", "", "env: {\"${{ 'LD_PRELOAD' }}\": library.so}\n", false},
		{"literal checkout loader key", "", "", "env: {\"${{ 'LD_PRELOAD' }}\": library.so}\n", "", false},
		{"literal checkout Git key", "", "", "env: {\"${{ 'GIT_WORK_TREE' }}\": /tmp}\n", "", false},
		{"unknown startup key", "", "", "", "env: {\"${{ vars.ENV_NAME }}\": ./setup.sh}\n", false},
		{"unknown checkout key", "", "", "env: {\"${{ vars.ENV_NAME }}\": /tmp}\n", "", false},
		{"literal ordinary key", "env: {\"${{ 'APP_ENV' }}\": test}\n", "", "", "", true},
		{"literal empty startup value", "", "", "", "env: {\"${{ 'BASH_ENV' }}\": \"${{ '' }}\"}\n", true},
		{"literal empty loader value", "", "", "", "env: {\"${{ 'LD_PRELOAD' }}\": \"${{ '' }}\"}\n", true},
		{"workflow Git templates", "env: {GIT_TEMPLATE_DIR: templates}\n", "", "", "", false},
		{"job Git templates", "", "env: {GIT_TEMPLATE_DIR: templates}\n", "", "", false},
		{"checkout Git templates", "", "", "env: {GIT_TEMPLATE_DIR: templates}\n", "", false},
		{"literal Git templates", "", "", "env: {\"${{ 'GIT_TEMPLATE_DIR' }}\": \"${{ 'templates' }}\"}\n", "", false},
		{"unknown Git templates", "", "", "env: {GIT_TEMPLATE_DIR: '${{ vars.TEMPLATES }}'}\n", "", false},
		{"empty Git templates", "", "", "env: {GIT_TEMPLATE_DIR: ''}\n", "", true},
		{"literal empty Git templates", "", "", "env: {GIT_TEMPLATE_DIR: \"${{ '' }}\"}\n", "", true},
		{"run-only Git templates", "", "", "", "env: {GIT_TEMPLATE_DIR: templates}\n", true},
		{"service mount", "", "services: {db: {image: postgres, volumes: ['/home/runner/work:/work']}}\n", "", "", false},
		{"service options", "", "services: {db: {image: postgres, options: '--mount type=bind,source=/home/runner/work,target=/work'}}\n", "", "", false},
		{"dynamic services", "", "services: ${{ fromJSON(vars.SERVICES) }}\n", "", "", false},
		{"dynamic service", "", "services: {db: '${{ fromJSON(vars.SERVICE) }}'}\n", "", "", false},
		{"dynamic volumes", "", "services: {db: {image: postgres, volumes: '${{ fromJSON(vars.VOLUMES) }}'}}\n", "", "", false},
		{"unmounted service", "", "services: {db: {image: postgres, ports: ['5432:5432']}}\n", "", "", true},
		{"empty service options", "", "services: {db: {image: postgres, volumes: [], options: ''}}\n", "", "", true},
		{"double quoted braces", "", "", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			indent := func(value, prefix string) string {
				if value == "" {
					return ""
				}
				return prefix + strings.ReplaceAll(strings.TrimSuffix(value, "\n"), "\n", "\n"+prefix) + "\n"
			}
			script := "./bad.sh"
			if tc.name == "double quoted braces" {
				script = `"./{good,bad}.sh"`
			}
			workflow := "on: push\n" + tc.workflow + "jobs:\n  test:\n    runs-on: ubuntu-latest\n" + indent(tc.job, "    ") +
				"    steps:\n      - uses: actions/checkout@v6\n" + indent(tc.checkout, "        ") +
				"      - run: |\n          " + script + "\n" + indent(tc.step, "        ")
			file := writeShellcheckFixture(t, root, ".github/workflows/runtime.yml", workflow)
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{file}, nil)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "executable-bit" {
					count++
				}
				if diagnostic.Rule == "syntax-check" {
					t.Fatalf("invalid fixture: %+v", diagnostic)
				}
			}
			if (count == 1) != tc.want || count > 1 {
				t.Fatalf("executable-bit findings %d; want finding=%v: %+v", count, tc.want, result.Diagnostics)
			}
		})
	}
}
