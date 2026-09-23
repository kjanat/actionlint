package actionlint

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func executableFixture(t *testing.T) (string, func(...string)) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git required")
	}
	root := t.TempDir()
	command := func(args ...string) {
		t.Helper()
		if out, err := repositoryGit(t.Context(), git, root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	command("init", "--quiet")
	command("config", "core.filemode", "false")
	for _, name := range []string{"bad.sh", "good.sh", "scripts/bad.sh", "scripts/good.sh", "space dir/bad.sh", "{good,bad}.sh"} {
		writeShellcheckFixture(t, root, name, "#!/bin/sh\necho script\n")
		command("add", "--", name)
		mode := "-x"
		if strings.HasSuffix(name, "good.sh") {
			mode = "+x"
		}
		command("update-index", "--chmod="+mode, "--", name)
	}
	writeShellcheckFixture(t, root, "untracked.sh", "#!/bin/sh\n")
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false}\n")
	return root, command
}

func TestExecutableBitWorkflows(t *testing.T) {
	root, git := executableFixture(t)
	link := writeShellcheckFixture(t, root, "link", "bad.sh")
	object, err := repositoryGit(t.Context(), "git", root, "hash-object", "-w", "--", link).Output()
	if err != nil {
		t.Fatal(err)
	}
	git("update-index", "--add", "--cacheinfo", "120000", strings.TrimSpace(string(object)), "link")
	index, err := os.Stat(filepath.Join(root, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, runner, defaults, steps string
		want                          string
	}{
		{"direct", "ubuntu-latest", "", "- run: ./bad.sh", "bad.sh"},
		{"false job", "ubuntu-latest", "if: false", "- run: ./bad.sh", ""},
		{"false expression job", "ubuntu-latest", "if: ${{ false }}", "- run: ./bad.sh", ""},
		{"literal zero job", "ubuntu-latest", "if: 0", "- run: ./bad.sh", ""},
		{"true job", "ubuntu-latest", "if: true", "- run: ./bad.sh", "bad.sh"},
		{"true expression job", "ubuntu-latest", "if: ${{ true }}", "- run: ./bad.sh", "bad.sh"},
		{"conditional job", "ubuntu-latest", "if: github.event_name == 'push'", "- run: ./bad.sh", "bad.sh"},
		{"parameter argument", "ubuntu-latest", "", "- run: ./bad.sh \"$GITHUB_SHA\"", "bad.sh"},
		{"braced parameter argument", "ubuntu-latest", "", "- run: ./bad.sh \"${GITHUB_SHA}\"", "bad.sh"},
		{"parameter prefix", "ubuntu-latest", "", "- run: FOO=$GITHUB_SHA ./bad.sh", "bad.sh"},
		{"command substitution argument", "ubuntu-latest", "", "- run: ./bad.sh \"$(chmod +x bad.sh)\"", ""},
		{"arithmetic argument", "ubuntu-latest", "", "- run: ./bad.sh \"$((COUNT++))\"", ""},
		{"capitalized bash", "ubuntu-latest", "", "- run: ./bad.sh\n  shell: Bash", "bad.sh"},
		{"uppercase sh default", "ubuntu-latest", "defaults:\n  run:\n    shell: SH\n", "- run: ./bad.sh", "bad.sh"},
		{"literal prefix assignment", "ubuntu-latest", "", "- run: FOO=bar ./bad.sh", "bad.sh"},
		{"quoted prefix assignment", "ubuntu-latest", "", "- run: FOO='two words' EMPTY= ./bad.sh", "bad.sh"},
		{"numeric variable prefix", "ubuntu-latest", "", "- run: RANDOM=1+2 SECONDS=1+2 OPTIND=1+2 ./bad.sh", "bad.sh"},
		{"readonly UID", "ubuntu-latest", "", "- run: UID=0 ./bad.sh", ""},
		{"readonly EUID", "ubuntu-latest", "", "- run: EUID=0 ./bad.sh", ""},
		{"readonly PPID", "ubuntu-latest", "", "- run: PPID=0 ./bad.sh", ""},
		{"readonly BASHOPTS", "ubuntu-latest", "", "- run: BASHOPTS=1 ./bad.sh", ""},
		{"readonly SHELLOPTS", "ubuntu-latest", "", "- run: SHELLOPTS=1 ./bad.sh", ""},
		{"readonly BASH_VERSINFO", "ubuntu-latest", "", "- run: BASH_VERSINFO=1 ./bad.sh", ""},
		{"sh may be bash", "ubuntu-latest", "", "- run: UID=0 ./bad.sh\n  shell: sh", ""},
		{"dynamic prefix assignment", "ubuntu-latest", "", "- run: FOO=$(chmod +x bad.sh) ./bad.sh", ""},
		{"prefix assignment cd", "ubuntu-latest", "", "- run: CDPATH=elsewhere cd scripts && ./bad.sh", ""},
		{"assignment only", "ubuntu-latest", "", "- run: FOO=bar; ./bad.sh", ""},
		{"missing invocation component", "ubuntu-latest", "", "- run: ./missing/../bad.sh", ""},
		{"file invocation component", "ubuntu-latest", "", "- run: ./good.sh/../bad.sh", ""},
		{"parent invocation component", "ubuntu-latest", "", "- run: ./scripts/../bad.sh", "bad.sh"},
		{"macOS", "macos-latest", "", "- run: ./bad.sh", "bad.sh"},
		{"indexed executable", "ubuntu-latest", "", "- run: ./good.sh", ""},
		{"interpreter", "ubuntu-latest", "", "- run: bash bad.sh", ""},
		{"exec wrapper", "ubuntu-latest", "", "- run: exec ./bad.sh", "bad.sh"},
		{"command wrapper", "ubuntu-latest", "", "- run: command ./bad.sh", "bad.sh"},
		{"exec separator", "ubuntu-latest", "", "- run: exec -- ./bad.sh", "bad.sh"},
		{"command separator", "ubuntu-latest", "", "- run: command -- ./bad.sh", "bad.sh"},
		{"exec parameter argument", "ubuntu-latest", "", "- run: exec ./bad.sh \"$GITHUB_SHA\"", "bad.sh"},
		{"command quoted path", "ubuntu-latest", "", "- run: command './space dir/bad.sh'", "space dir/bad.sh"},
		{"exec unsupported flags", "ubuntu-latest", "", "- run: exec -z ./bad.sh", ""},
		{"command lookup", "ubuntu-latest", "", "- run: command -v ./bad.sh", ""},
		{"command verbose lookup", "ubuntu-latest", "", "- run: command -V ./bad.sh", ""},
		{"command unsupported flags", "ubuntu-latest", "", "- run: command -z ./bad.sh", ""},
		{"exec interpreter", "ubuntu-latest", "", "- run: exec bash bad.sh", ""},
		{"exec executable", "ubuntu-latest", "", "- run: exec ./good.sh", ""},
		{"exec missing command", "ubuntu-latest", "", "- run: exec --", ""},
		{"command dynamic operand", "ubuntu-latest", "", "- run: command \"$SCRIPT\"", ""},
		{"exec substitution argument", "ubuntu-latest", "", "- run: exec ./bad.sh \"$(chmod +x bad.sh)\"", ""},
		{"source", "ubuntu-latest", "", "- run: . ./bad.sh", ""},
		{"untracked", "ubuntu-latest", "", "- run: ./untracked.sh", ""},
		{"windows runner", "windows-latest", "", "- run: ./bad.sh\n  shell: bash", ""},
		{"unknown runner", "self-hosted", "", "- run: ./bad.sh", ""},
		{"self-hosted Unix", "[self-hosted, linux]", "", "- run: ./bad.sh", ""},
		{"OS-only runner", "linux", "", "- run: ./bad.sh", ""},
		{"self-hosted hosted label", "[self-hosted, ubuntu-latest]", "", "- run: ./bad.sh", ""},
		{"custom Unix runner", "ubuntu-custom", "", "- run: ./bad.sh", ""},
		{"grouped runner", "{group: build, labels: ubuntu-latest}", "", "- run: ./bad.sh", ""},
		{"expression grouped runner", "${{ fromJSON('{\"group\":\"build\",\"labels\":\"ubuntu-latest\"}') }}", "", "- run: ./bad.sh", ""},
		{"unresolved runner label", "[ubuntu-latest, '${{ matrix.label }}']", "", "- run: ./bad.sh", ""},
		{"literal hosted expression", "${{ 'ubuntu-latest' }}", "", "- run: ./bad.sh", "bad.sh"},
		{"job directory", "ubuntu-latest", "defaults:\n  run:\n    working-directory: scripts\n", "- run: ./bad.sh", "scripts/bad.sh"},
		{"empty step directory", "ubuntu-latest", "defaults:\n  run:\n    working-directory: scripts\n", "- run: ./bad.sh\n  working-directory: ''", "bad.sh"},
		{"step directory", "ubuntu-latest", "", "- run: ./bad.sh\n  working-directory: scripts", "scripts/bad.sh"},
		{"quoted directory", "ubuntu-latest", "", "- run: cd 'space dir' && ./bad.sh", "space dir/bad.sh"},
		{"cd resolves", "ubuntu-latest", "", "- run: cd scripts && ./bad.sh", "scripts/bad.sh"},
		{"missing cd component", "ubuntu-latest", "", "- run: cd missing/.. && ./bad.sh", ""},
		{"file cd component", "ubuntu-latest", "", "- run: cd bad.sh/.. && ./bad.sh", ""},
		{"cd parent resolves", "ubuntu-latest", "", "- run: cd scripts/.. && ./bad.sh", "bad.sh"},
		{"cd resets each step", "ubuntu-latest", "", "- run: cd scripts\n- run: ./bad.sh", "bad.sh"},
		{"earlier chmod", "ubuntu-latest", "", "- run: chmod +x bad.sh\n- run: ./bad.sh", ""},
		{"symlink chmod", "ubuntu-latest", "", "- run: chmod +x link\n- run: ./bad.sh", ""},
		{"skipped chmod", "ubuntu-latest", "", "- run: chmod +x bad.sh\n  if: ${{ false }}\n- run: ./bad.sh", "bad.sh"},
		{"bare skipped chmod", "ubuntu-latest", "", "- run: chmod +x bad.sh\n  if: false\n- run: ./bad.sh", "bad.sh"},
		{"skipped opaque run", "ubuntu-latest", "", "- run: ./good.sh\n  if: false\n- run: ./bad.sh", "bad.sh"},
		{"skipped invocation", "ubuntu-latest", "", "- run: ./bad.sh\n  if: false", ""},
		{"literal zero invocation", "ubuntu-latest", "", "- run: ./bad.sh\n  if: 0", ""},
		{"literal negative zero invocation", "ubuntu-latest", "", "- run: ./bad.sh\n  if: ${{ -0 }}", ""},
		{"literal null invocation", "ubuntu-latest", "", "- run: ./bad.sh\n  if: ${{ null }}", ""},
		{"literal empty string invocation", "ubuntu-latest", "", "- run: ./bad.sh\n  if: ${{ '' }}", ""},
		{"literal skipped chmod", "ubuntu-latest", "", "- run: chmod +x bad.sh\n  if: 0\n- run: ./bad.sh", "bad.sh"},
		{"literal nonzero condition", "ubuntu-latest", "", "- run: echo ok\n  if: ${{ -1 }}\n- run: ./bad.sh", "bad.sh"},
		{"literal nonempty condition", "ubuntu-latest", "", "- run: echo ok\n  if: ${{ 'false' }}\n- run: ./bad.sh", "bad.sh"},
		{"literal array condition", "ubuntu-latest", "", "- run: echo ok\n  if: ${{ fromJSON('[]') }}\n- run: ./bad.sh", "bad.sh"},
		{"literal object condition", "ubuntu-latest", "", "- run: echo ok\n  if: ${{ fromJSON('{}') }}\n- run: ./bad.sh", "bad.sh"},
		{"conditional invocation", "ubuntu-latest", "", "- run: ./bad.sh\n  if: github.event_name == 'push'", "bad.sh"},
		{"always invocation", "ubuntu-latest", "", "- run: ./bad.sh\n  if: always()", ""},
		{"failure invocation", "ubuntu-latest", "", "- run: ./bad.sh\n  if: failure()", ""},
		{"cancelled invocation", "ubuntu-latest", "", "- run: ./bad.sh\n  if: cancelled()", ""},
		{"negated success invocation", "ubuntu-latest", "", "- run: ./bad.sh\n  if: ${{ !success() }}", ""},
		{"success invocation", "ubuntu-latest", "", "- run: ./bad.sh\n  if: success()", "bad.sh"},
		{"success conjunction", "ubuntu-latest", "", "- run: ./bad.sh\n  if: success() && github.event_name == 'push'", "bad.sh"},
		{"success disjunction", "ubuntu-latest", "", "- run: ./bad.sh\n  if: success() || failure()", ""},
		{"quoted status function", "ubuntu-latest", "", "- run: ./bad.sh\n  if: contains(github.event_name, 'failure()')", "bad.sh"},
		{"conditional invocation invalidates following", "ubuntu-latest", "", "- run: ./bad.sh\n  if: github.event_name == 'push'\n- run: ./bad.sh", "bad.sh"},
		{"conditional run invalidates", "ubuntu-latest", "", "- run: echo hello\n  if: github.event_name == 'push'\n- run: ./bad.sh", ""},
		{"constant true run", "ubuntu-latest", "", "- run: ./bad.sh\n  if: ${{ true }}", "bad.sh"},
		{"same step chmod", "ubuntu-latest", "", "- run: chmod +x bad.sh && ./bad.sh", ""},
		{"recursive chmod after mode", "ubuntu-latest", "", "- run: chmod +x -R scripts && ./scripts/bad.sh", ""},
		{"recursive chmod after operand", "ubuntu-latest", "", "- run: chmod +x scripts --recursive && ./scripts/bad.sh", ""},
		{"directory chmod before cd", "ubuntu-latest", "", "- run: chmod 000 scripts && cd scripts && ./bad.sh", ""},
		{"directory chmod before invocation", "ubuntu-latest", "", "- run: chmod 000 scripts && ./scripts/bad.sh", ""},
		{"chmod operand separator", "ubuntu-latest", "", "- run: chmod +x -- good.sh && ./bad.sh", "bad.sh"},
		{"chmod missing operand", "ubuntu-latest", "", "- run: chmod +x && ./bad.sh", ""},
		{"chmod missing operand after separator", "ubuntu-latest", "", "- run: chmod +x -- && ./bad.sh", ""},
		{"chmod missing file", "ubuntu-latest", "", "- run: chmod +x missing.sh && ./bad.sh", ""},
		{"chmod untracked file", "ubuntu-latest", "", "- run: chmod +x untracked.sh && ./bad.sh", ""},
		{"chmod missing later file", "ubuntu-latest", "", "- run: chmod +x good.sh missing.sh && ./bad.sh", ""},
		{"invalid chmod mode", "ubuntu-latest", "", "- run: chmod nonsense good.sh && ./bad.sh", ""},
		{"invalid chmod octal", "ubuntu-latest", "", "- run: chmod 888 good.sh && ./bad.sh", ""},
		{"invalid chmod symbolic", "ubuntu-latest", "", "- run: chmod u+invalid good.sh && ./bad.sh", ""},
		{"valid chmod octal", "ubuntu-latest", "", "- run: chmod 0644 good.sh && ./bad.sh", "bad.sh"},
		{"valid chmod clauses", "ubuntu-latest", "", "- run: chmod u=rw,g=u,o-rwx good.sh && ./bad.sh", "bad.sh"},
		{"later chmod", "ubuntu-latest", "", "- run: ./bad.sh\n- run: chmod +x bad.sh", "bad.sh"},
		{"other file chmod", "ubuntu-latest", "", "- run: chmod +x good.sh\n- run: ./bad.sh", "bad.sh"},
		{"dynamic chmod", "ubuntu-latest", "", "- run: chmod +x \"$SCRIPT\"\n- run: ./bad.sh", ""},
		{"unknown action", "ubuntu-latest", "", "- uses: actions/setup-node@v6\n- run: ./bad.sh", ""},
		{"skipped opaque action", "ubuntu-latest", "", "- uses: actions/setup-node@v6\n  if: false\n- run: ./bad.sh", "bad.sh"},
		{"skipped checkout", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  if: ${{ false }}\n- run: ./bad.sh", "bad.sh"},
		{"true checkout", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  if: ${{ true }}\n- run: ./bad.sh", "bad.sh"},
		{"success checkout", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  if: success()\n- run: ./bad.sh", "bad.sh"},
		{"success expression checkout", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  if: ${{ success() }}\n- run: ./bad.sh", "bad.sh"},
		{"conditional success checkout", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  if: success() && github.event_name == 'push'\n- run: ./bad.sh", ""},
		{"title case clean", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {clean: 'True'}\n- run: ./bad.sh", "bad.sh"},
		{"uppercase clean", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {clean: 'TRUE'}\n- run: ./bad.sh", "bad.sh"},
		{"padded clean", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {clean: ' true '}\n- run: ./bad.sh", "bad.sh"},
		{"empty clean", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {clean: ''}\n- run: ./bad.sh", "bad.sh"},
		{"expression uppercase clean", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {clean: \"${{ 'TRUE' }}\"}\n- run: ./bad.sh", "bad.sh"},
		{"uppercase false clean", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {clean: 'FALSE'}\n- run: ./bad.sh", ""},
		{"empty checkout path", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {path: ''}\n- run: ./bad.sh", "bad.sh"},
		{"empty checkout path expression", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with:\n    path: ${{ '' }}\n- run: ./bad.sh", "bad.sh"},
		{"tolerated checkout failure", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  continue-on-error: true\n- run: ./bad.sh", ""},
		{"expression tolerated checkout failure", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  continue-on-error: ${{ true }}\n- run: ./bad.sh", ""},
		{"unknown tolerated checkout failure", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  continue-on-error: ${{ github.event_name == 'push' }}\n- run: ./bad.sh", ""},
		{"checkout failure stops job", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  continue-on-error: false\n- run: ./bad.sh", "bad.sh"},
		{"expression checkout failure stops job", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  continue-on-error: ${{ false }}\n- run: ./bad.sh", "bad.sh"},
		{"checkout after unknown repository settings", "ubuntu-latest", "", "- run: git config core.fileMode false && chmod +x bad.sh\n- uses: actions/checkout@v6\n- run: ./bad.sh", ""},
		{"checkout after opaque action", "ubuntu-latest", "", "- uses: actions/setup-node@v6\n- uses: actions/checkout@v6\n- run: ./bad.sh", ""},
		{"unknown checkout condition", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  if: github.event_name == 'push'\n- run: ./bad.sh", ""},
		{"opaque shell", "ubuntu-latest", "", "- run: bash -c 'chmod +x bad.sh'\n- run: ./bad.sh", ""},
		{"CDPATH", "ubuntu-latest", "", "- run: cd scripts && ./bad.sh\n  env:\n    CDPATH: elsewhere", ""},
		{"BASH_ENV", "ubuntu-latest", "", "- run: ./bad.sh\n  env:\n    BASH_ENV: setup.sh", ""},
		{"step SHELLOPTS", "ubuntu-latest", "", "- run: ./bad.sh \"$UNSET\"\n  env: {SHELLOPTS: nounset}", ""},
		{"job SHELLOPTS", "ubuntu-latest", "env: {SHELLOPTS: nounset}", "- run: ./bad.sh \"$UNSET\"", ""},
		{"step BASHOPTS", "ubuntu-latest", "", "- run: ./bad.sh missing-*\n  env: {BASHOPTS: failglob}", ""},
		{"empty SHELLOPTS", "ubuntu-latest", "", "- run: ./bad.sh\n  env: {SHELLOPTS: ''}", "bad.sh"},
		{"step PATH", "ubuntu-latest", "", "- run: chmod +x good.sh\n  env: {PATH: tools}\n- run: ./bad.sh", ""},
		{"empty step PATH", "ubuntu-latest", "", "- run: chmod +x good.sh\n  env: {PATH: ''}\n- run: ./bad.sh", ""},
		{"job PATH", "ubuntu-latest", "env: {PATH: tools}", "- run: chmod +x good.sh\n- run: ./bad.sh", ""},
		{"exported Bash function", "ubuntu-latest", "", "- run: chmod +x good.sh\n  env: {'BASH_FUNC_chmod%%': '() { return 0; }'}\n- run: ./bad.sh", ""},
		{"job exported Bash function", "ubuntu-latest", "env: {'BASH_FUNC_chmod%%': '() { return 0; }'}", "- run: chmod +x good.sh\n- run: ./bad.sh", ""},
		{"printf assignment", "ubuntu-latest", "", "- run: printf -v CDPATH elsewhere; cd scripts; ./bad.sh", ""},
		{"brace expansion", "ubuntu-latest", "", "- run: ./{good,bad}.sh", ""},
		{"literal braces", "ubuntu-latest", "", "- run: \"'./{good,bad}.sh'\"", "{good,bad}.sh"},
		{"literal text", "ubuntu-latest", "", "- run: echo './bad.sh'", ""},
		{"Unicode before invocation", "ubuntu-latest", "", "- run: echo é; ./bad.sh", "bad.sh"},
		{"executable guard", "ubuntu-latest", "", "- run: test -x ./bad.sh && ./bad.sh", ""},
		{"bracket executable guard", "ubuntu-latest", "", "- run: '[ -x ./bad.sh ] && ./bad.sh'", ""},
		{"false condition", "ubuntu-latest", "", "- run: 'false && ./bad.sh'", ""},
		{"quoted script", "ubuntu-latest", "", "- run: \"'./space dir/bad.sh'\"", "space dir/bad.sh"},
		{"heredoc", "ubuntu-latest", "", "- run: |\n    cat <<'EOF'\n    ./bad.sh\n    EOF", ""},
		{"function declaration", "ubuntu-latest", "", "- run: 'f() { ./bad.sh; }'", ""},
		{"shell conditional", "ubuntu-latest", "", "- run: 'if true; then chmod +x bad.sh; fi; ./bad.sh'", ""},
		{"expression injection", "ubuntu-latest", "", "- run: '${{ github.event.inputs.script }}; ./bad.sh'", ""},
		{"substitution", "ubuntu-latest", "", "- run: echo $(chmod +x bad.sh) && ./bad.sh", ""},
		{"shell background", "ubuntu-latest", "", "- run: chmod +x bad.sh & ./bad.sh", ""},
		{"background step", "ubuntu-latest", "", "- run: chmod +x bad.sh\n  background: true\n- run: ./bad.sh", ""},
		{"expression false background", "ubuntu-latest", "", "- run: ./bad.sh\n  background: ${{ false }}", "bad.sh"},
		{"expression true background", "ubuntu-latest", "", "- run: ./bad.sh\n  background: ${{ true }}", ""},
		{"unknown background", "ubuntu-latest", "", "- run: ./bad.sh\n  background: ${{ github.event_name == 'push' }}", ""},
		{"parallel group", "ubuntu-latest", "", "- parallel:\n    - run: chmod +x bad.sh\n    - run: ./bad.sh\n- run: ./bad.sh", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := "on: push\njobs:\n  test:\n    runs-on: " + tc.runner + "\n"
			if tc.defaults != "" {
				job += "    " + strings.ReplaceAll(strings.TrimSuffix(tc.defaults, "\n"), "\n", "\n    ") + "\n"
			}
			job += "    steps:\n      - uses: actions/checkout@v6\n      " + strings.ReplaceAll(tc.steps, "\n", "\n      ") + "\n"
			workflow := writeShellcheckFixture(t, root, ".github/workflows/test.yml", job)
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var findings []Diagnostic
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "executable-bit" {
					findings = append(findings, diagnostic)
				}
			}
			if tc.want == "" {
				if len(findings) != 0 {
					t.Fatalf("false positive: %+v", findings)
				}
			} else {
				if len(findings) != 1 || !strings.Contains(findings[0].Message, `script "`+tc.want+`"`) {
					t.Fatalf("expected non-executable %q: %+v", tc.want, result.Diagnostics)
				}
				line := strings.Split(job, "\n")[findings[0].Start.Line-1]
				if !strings.Contains(line, filepath.Base(tc.want)) {
					t.Fatalf("diagnostic mapped to wrong line: %+v", findings[0])
				}
				if tc.name == "direct" && findings[0].Start != (DiagnosticPosition{7, 14}) {
					t.Fatalf("direct call location: %+v", findings[0].Start)
				}
				if tc.name == "Unicode before invocation" && findings[0].Start != (DiagnosticPosition{7, 22}) {
					t.Fatalf("Unicode call location: %+v", findings[0].Start)
				}
				if !slices.ContainsFunc(result.Inputs, func(path string) bool {
					info, err := os.Stat(path)
					return err == nil && os.SameFile(index, info)
				}) {
					t.Fatalf("Git index absent from inputs: %v", result.Inputs)
				}
			}
		})
	}
}

func TestExecutableBitReusableWorkflowRepository(t *testing.T) {
	root, _ := executableFixture(t)
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		event string
		want  bool
	}{
		{"workflow_call", false},
		{"{workflow_call: {}}", false},
		{"[push, workflow_call]", false},
		{"push", true},
		{"workflow_dispatch", true},
	} {
		t.Run(tc.event, func(t *testing.T) {
			workflow := writeShellcheckFixture(t, root, ".github/workflows/test.yml", "on: "+tc.event+"\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n      - run: ./bad.sh\n")
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" })
			if found != tc.want {
				t.Fatalf("checkout repository assumption for %s: %+v", tc.event, result.Diagnostics)
			}
		})
	}
}

func TestExecutableBitWorkflowShellOptions(t *testing.T) {
	root, _ := executableFixture(t)
	workflow := writeShellcheckFixture(t, root, ".github/workflows/test.yml", "on: push\nenv: {SHELLOPTS: nounset}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n      - run: ./bad.sh \"$UNSET\"\n")
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Files([]string{workflow}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" }) {
		t.Fatalf("shell startup options ignored: %+v", result.Diagnostics)
	}
}

func TestExecutableBitIndexTraversal(t *testing.T) {
	snapshot := &gitModeSnapshot{modes: map[string]string{"link": "120000", "submodule": "160000", "bad.sh": "100644", "scripts/bad.sh": "100644"}}
	for _, tc := range []struct {
		path, checkout string
		want           bool
	}{
		{"./bad.sh", "", true},
		{"scripts/../bad.sh", "", true},
		{"missing/../bad.sh", "", false},
		{"scripts/", "", true},
		{"missing/", "", false},
		{"bad.sh/", "", false},
		{"missing/../source/bad.sh", "source", false},
		{"link/../bad.sh", "", false},
		{"submodule/../bad.sh", "", false},
		{"source/link/../bad.sh", "source", false},
		{".source/link/../bad.sh", ".source", false},
		{"scripts/bad.sh/../bad.sh", "", false},
		{"../bad.sh", "", false},
	} {
		if got := snapshot.ordinaryTraversal(tc.path, tc.checkout); got != tc.want {
			t.Errorf("traversal %q under %q: got %v", tc.path, tc.checkout, got)
		}
	}
}

func TestExecutableBitCheckoutPrefixedInvocation(t *testing.T) {
	root, _ := executableFixture(t)
	workflow := writeShellcheckFixture(t, root, ".github/workflows/test.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n        with:\n          path: source\n      - run: ./source/bad.sh\n")
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Files([]string{workflow}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" }) {
		t.Fatalf("workspace invocation of checkout-prefixed script missed: %+v", result.Diagnostics)
	}
}

func TestExecutableBitCheckoutAndFreshIndex(t *testing.T) {
	root, git := executableFixture(t)
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, with, directory string
		want                  bool
	}{
		{"self checkout subdirectory", "path: source", "source/scripts", true},
		{"clean true", "clean: true", "", true},
		{"clean expression true", "clean: ${{ true }}", "", true},
		{"clean string expression true", "clean: ${{ 'true' }}", "", true},
		{"clean false", "clean: false", "", false},
		{"clean expression false", "clean: ${{ false }}", "", false},
		{"clean unknown expression", "clean: ${{ inputs.clean }}", "", false},
		{"wrong checkout directory", "path: source", "scripts", false},
		{"other repository", "repository: owner/other", "", false},
		{"other ref", "ref: other-branch", "", false},
		{"dynamic path", "path: ${{ github.event.inputs.path }}", "", false},
		{"checkout outside workspace", "path: ../source", "../source", false},
		{"working tree updated index", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n"
			if tc.with != "" {
				workflow += "        with:\n          " + tc.with + "\n"
			}
			workflow += "      - run: ./bad.sh\n"
			if tc.directory != "" {
				workflow += "        working-directory: " + tc.directory + "\n"
			}
			file := writeShellcheckFixture(t, root, ".github/workflows/test.yml", workflow)
			result, err := session.Files([]string{file}, nil)
			if err != nil {
				t.Fatal(err)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" })
			if found != tc.want {
				t.Fatalf("unexpected executable-bit result: %+v", result.Diagnostics)
			}
		})
	}
	git("update-index", "--chmod=+x", "bad.sh")
	result, err := session.Files([]string{filepath.Join(root, ".github/workflows/test.yml")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" }) {
		t.Fatalf("session retained stale Git index: %+v", result.Diagnostics)
	}
}

func TestGitIndexModes(t *testing.T) {
	modes, err := parseGitModes("100644 abc 0\tspace name.sh\x00100755 def 0\tok.sh\x00120000 abc 0\tlink\x00100644 abc 1\tconflict.sh\x00100755 def 2\tconflict.sh\x00")
	if err != nil || modes["space name.sh"] != "100644" || modes["ok.sh"] != "100755" || modes["link"] != "120000" || modes["conflict.sh"] != "unmerged" {
		t.Fatalf("incorrect index modes: %v, %v", modes, err)
	}
}
