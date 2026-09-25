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
	writeShellcheckFixture(t, root, "intent.sh", "#!/bin/sh\n")
	command("add", "-N", "intent.sh")
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
		{"intent to add", "ubuntu-latest", "", "- run: ./intent.sh", ""},
		{"redirect Git directory", "ubuntu-latest", "", "- run: ./bad.sh > .git", ""},
		{"append Git directory", "ubuntu-latest", "", "- run: ./bad.sh >> .git", ""},
		{"redirect nested checkout Git directory", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {path: source}\n- run: ./source/bad.sh > source/.git", ""},
		{"redirect macOS Git directory", "macos-latest", "", "- run: ./bad.sh > .GIT", ""},
		{"redirect ordinary dotgit file", "ubuntu-latest", "", "- run: ./bad.sh > scripts/.git", "bad.sh"},
		{"checkout replaces tracked file", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {path: bad.sh}\n- run: ./bad.sh/bad.sh", "bad.sh"},
		{"legacy checkout v1", "ubuntu-latest", "", "- uses: actions/checkout@v1\n- run: ./bad.sh", "bad.sh"},
		{"legacy checkout v2", "ubuntu-latest", "", "- uses: actions/checkout@v2\n- run: ./bad.sh", "bad.sh"},
		{"legacy checkout v3", "ubuntu-latest", "", "- uses: actions/checkout@v3\n- run: ./bad.sh", "bad.sh"},
		{"legacy checkout case", "ubuntu-latest", "", "- uses: Actions/Checkout@v3\n- run: ./bad.sh", "bad.sh"},
		{"legacy checkout patch", "ubuntu-latest", "", "- uses: actions/checkout@v3.6.0\n- run: ./bad.sh", "bad.sh"},
		{"legacy checkout SHA", "ubuntu-latest", "", "- uses: actions/checkout@f43a0e5ff2bd294095638e18286ca9a3d1956744\n- run: ./bad.sh", "bad.sh"},
		{"empty checkout ref", "ubuntu-latest", "", "- uses: actions/checkout@\n- run: ./bad.sh", ""},
		{"supported checkout", "ubuntu-latest", "", "- uses: actions/checkout@v4\n- run: ./bad.sh", "bad.sh"},
		{"negated direct Bash", "ubuntu-latest", "", "- run: '! ./bad.sh'", "bad.sh"},
		{"negated direct sh", "ubuntu-latest", "", "- run: '! ./bad.sh'\n  shell: sh", "bad.sh"},
		{"negated builtin invalidates state", "ubuntu-latest", "", "- run: '! true && ./bad.sh'", ""},
		{"negated executable", "ubuntu-latest", "", "- run: '! ./good.sh; ./bad.sh'", ""},
		{"OR left invocation", "ubuntu-latest", "", "- run: ./bad.sh || echo tolerated", "bad.sh"},
		{"OR left invocation sh", "ubuntu-latest", "", "- run: ./bad.sh || echo tolerated\n  shell: sh", "bad.sh"},
		{"OR conditional invocation", "ubuntu-latest", "", "- run: true || ./bad.sh", ""},
		{"OR continuation unknown", "ubuntu-latest", "", "- run: true || echo tolerated; ./bad.sh", ""},
		{"redirect stderr null", "ubuntu-latest", "", "- run: ./bad.sh 2>/dev/null", "bad.sh"},
		{"redirect stdout file", "ubuntu-latest", "", "- run: ./bad.sh >output.log", "bad.sh"},
		{"redirect append file", "ubuntu-latest", "", "- run: ./bad.sh >>output.log", "bad.sh"},
		{"redirect input file", "ubuntu-latest", "", "- run: ./bad.sh <good.sh", "bad.sh"},
		{"redirect duplicate stderr", "ubuntu-latest", "", "- run: ./bad.sh 2>&1", "bad.sh"},
		{"redirect wrapped call", "ubuntu-latest", "", "- run: command ./bad.sh &>/dev/null", "bad.sh"},
		{"redirect missing input", "ubuntu-latest", "", "- run: ./bad.sh <missing.txt", ""},
		{"redirect missing parent", "ubuntu-latest", "", "- run: ./bad.sh >missing/output.log", ""},
		{"redirect directory", "ubuntu-latest", "", "- run: ./bad.sh >scripts", ""},
		{"redirect unopened descriptor", "ubuntu-latest", "", "- run: ./bad.sh 2>&9", ""},
		{"redirect oversized descriptor", "ubuntu-latest", "", "- run: ./bad.sh 999999999999999999999999999999>/dev/null", ""},
		{"redirect changed input permissions", "ubuntu-latest", "", "- run: chmod 000 good.sh; ./bad.sh <good.sh", ""},
		{"redirect changed output permissions", "ubuntu-latest", "", "- run: chmod a-w good.sh; ./bad.sh >good.sh", ""},
		{"redirect command substitution", "ubuntu-latest", "", "- run: ./bad.sh >\"$(chmod +x bad.sh)\"", ""},
		{"redirect process substitution", "ubuntu-latest", "", "- run: ./bad.sh > >(chmod +x bad.sh)", ""},
		{"redirect builtin invalidates state", "ubuntu-latest", "", "- run: echo changed >output.log; ./bad.sh", ""},
		{"direct step PATH", "ubuntu-latest", "", "- run: ./bad.sh\n  env: {PATH: /usr/bin}", "bad.sh"},
		{"direct job PATH", "ubuntu-latest", "env: {PATH: /usr/bin}", "- run: ./bad.sh", "bad.sh"},
		{"wrapped step PATH", "ubuntu-latest", "", "- run: command ./bad.sh\n  env: {PATH: /usr/bin}", "bad.sh"},
		{"exec step PATH", "ubuntu-latest", "", "- run: exec ./bad.sh\n  env: {PATH: /usr/bin}", "bad.sh"},
		{"redirect step PATH", "ubuntu-latest", "", "- run: ./bad.sh 2>/dev/null\n  env: {PATH: /usr/bin}", "bad.sh"},
		{"cd step PATH", "ubuntu-latest", "", "- run: cd scripts && ./bad.sh\n  env: {PATH: /usr/bin}", "scripts/bad.sh"},
		{"PATH and startup script", "ubuntu-latest", "", "- run: ./bad.sh\n  env: {PATH: /usr/bin, BASH_ENV: setup.sh}", ""},
		{"run Git override cannot move checkout", "ubuntu-latest", "", "- run: ./bad.sh\n  env: {GIT_WORK_TREE: /tmp}", "bad.sh"},
		{"PATH chmod then direct", "ubuntu-latest", "", "- run: chmod +x good.sh && ./bad.sh\n  env: {PATH: tools}", ""},
		{"false job", "ubuntu-latest", "if: false", "- run: ./bad.sh", ""},
		{"false expression job", "ubuntu-latest", "if: ${{ false }}", "- run: ./bad.sh", ""},
		{"contradictory status job", "ubuntu-latest", "if: success() && failure()", "- run: ./bad.sh", ""},
		{"contradictory expression job", "ubuntu-latest", "if: ${{ failure() && success() }}", "- run: ./bad.sh", ""},
		{"contradictory dynamic job", "ubuntu-latest", "if: github.event_name == 'push' && success() && failure()", "- run: ./bad.sh", ""},
		{"reachable status job", "ubuntu-latest", "if: success() || failure()", "- run: ./bad.sh", "bad.sh"},
		{"unknown status job", "ubuntu-latest", "if: success() && github.event_name == 'push'", "- run: ./bad.sh", "bad.sh"},
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
		{"Ubuntu sh UID", "ubuntu-latest", "", "- run: UID=0 ./bad.sh\n  shell: sh", "bad.sh"},
		{"Ubuntu default sh UID", "ubuntu-latest", "defaults: {run: {shell: sh}}", "- run: UID=0 ./bad.sh", "bad.sh"},
		{"Ubuntu expression sh UID", "${{ 'ubuntu-latest' }}", "", "- run: UID=0 ./bad.sh\n  shell: sh", "bad.sh"},
		{"Ubuntu sh Bash names", "ubuntu-latest", "", "- run: EUID=0 PPID=0 BASHOPTS=1 SHELLOPTS=1 BASH_VERSINFO=1 ./bad.sh\n  shell: sh", "bad.sh"},
		{"macOS sh may be bash", "macos-latest", "", "- run: UID=0 ./bad.sh\n  shell: sh", ""},
		{"pipeline left", "ubuntu-latest", "", "- run: ./bad.sh | true", "bad.sh"},
		{"pipeline right", "ubuntu-latest", "", "- run: echo text | ./bad.sh", "bad.sh"},
		{"pipeline middle", "ubuntu-latest", "", "- run: echo text | ./bad.sh | true", "bad.sh"},
		{"pipeline sh", "ubuntu-latest", "", "- run: ./bad.sh | true\n  shell: sh", "bad.sh"},
		{"pipeline stderr", "ubuntu-latest", "", "- run: ./bad.sh |& true", "bad.sh"},
		{"pipeline mutating sibling", "ubuntu-latest", "", "- run: ./bad.sh | chmod +x bad.sh", ""},
		{"pipeline opaque sibling", "ubuntu-latest", "", "- run: ./bad.sh | cat", ""},
		{"pipeline executable sibling", "ubuntu-latest", "", "- run: ./good.sh | ./bad.sh", ""},
		{"pipeline redirected sibling", "ubuntu-latest", "", "- run: ./bad.sh | true >good.sh", ""},
		{"pipeline sibling substitution", "ubuntu-latest", "", "- run: ./bad.sh | echo $(chmod +x bad.sh)", ""},
		{"pipeline clears following state", "ubuntu-latest", "", "- run: true | true; ./bad.sh", ""},
		{"pipeline changed mode", "ubuntu-latest", "", "- run: chmod +x bad.sh; ./bad.sh | true", ""},
		{"dynamic prefix assignment", "ubuntu-latest", "", "- run: FOO=$(chmod +x bad.sh) ./bad.sh", ""},
		{"prefix assignment cd", "ubuntu-latest", "", "- run: CDPATH=elsewhere cd scripts && ./bad.sh", ""},
		{"assignment only", "ubuntu-latest", "", "- run: FOO=bar; ./bad.sh", ""},
		{"missing invocation component", "ubuntu-latest", "", "- run: ./missing/../bad.sh", ""},
		{"file invocation component", "ubuntu-latest", "", "- run: ./good.sh/../bad.sh", ""},
		{"parent invocation component", "ubuntu-latest", "", "- run: ./scripts/../bad.sh", "bad.sh"},
		{"macOS", "macos-latest", "", "- run: ./bad.sh", "bad.sh"},
		{"macOS case folded file", "macos-latest", "", "- run: ./BAD.SH", "bad.sh"},
		{"macOS case folded directory", "macos-latest", "", "- run: ./SCRIPTS/BAD.SH", "scripts/bad.sh"},
		{"macOS case folded cd", "macos-latest", "", "- run: cd SCRIPTS && ./BAD.SH", "scripts/bad.sh"},
		{"macOS case folded default directory", "macos-latest", "defaults:\n  run:\n    working-directory: SCRIPTS\n", "- run: ./BAD.SH", "scripts/bad.sh"},
		{"macOS case folded checkout", "macos-latest", "", "- uses: actions/checkout@v6\n  with: {path: source}\n- run: ./SOURCE/BAD.SH", "bad.sh"},
		{"macOS checkout ancestor cd", "macos-latest", "", "- uses: actions/checkout@v6\n  with: {path: source}\n- run: cd SOURCE/.. && ./SOURCE/BAD.SH", "bad.sh"},
		{"checkout ancestor cd", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {path: source}\n- run: cd source/.. && ./source/bad.sh", "bad.sh"},
		{"checkout workspace cd", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {path: source}\n- run: cd . && ./source/bad.sh", "bad.sh"},
		{"macOS case folded chmod", "macos-latest", "", "- run: chmod +x BAD.SH && ./bad.sh", ""},
		{"macOS case folded redirect", "macos-latest", "", "- run: ./BAD.SH <GOOD.SH", "bad.sh"},
		{"macOS case folded symlink", "macos-latest", "", "- run: ./LINK/../BAD.SH", ""},
		{"Linux case sensitive file", "ubuntu-latest", "", "- run: ./BAD.SH", ""},
		{"Linux case sensitive directory", "ubuntu-latest", "", "- run: ./SCRIPTS/bad.sh", ""},
		{"indexed executable", "ubuntu-latest", "", "- run: ./good.sh", ""},
		{"interpreter", "ubuntu-latest", "", "- run: bash bad.sh", ""},
		{"exec wrapper", "ubuntu-latest", "", "- run: exec ./bad.sh", "bad.sh"},
		{"command wrapper", "ubuntu-latest", "", "- run: command ./bad.sh", "bad.sh"},
		{"exec separator", "ubuntu-latest", "", "- run: exec -- ./bad.sh", "bad.sh"},
		{"sh exec separator", "ubuntu-latest", "", "- run: exec -- ./bad.sh\n  shell: sh", ""},
		{"sh command separator", "ubuntu-latest", "", "- run: command -- ./bad.sh\n  shell: sh", "bad.sh"},
		{"sh exec direct", "ubuntu-latest", "", "- run: exec ./bad.sh\n  shell: sh", "bad.sh"},
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
		{"multiple hosted images", "[ubuntu-22.04, ubuntu-24.04]", "", "- run: ./bad.sh", ""},
		{"expression multiple hosted images", "${{ fromJSON('[\"ubuntu-22.04\",\"ubuntu-24.04\"]') }}", "", "- run: ./bad.sh", ""},
		{"single hosted image list", "[ubuntu-latest]", "", "- run: ./bad.sh", "bad.sh"},
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
		{"contradictory invocation", "ubuntu-latest", "", "- run: ./bad.sh\n  if: success() && failure()", ""},
		{"contradictory opaque action", "ubuntu-latest", "", "- uses: actions/setup-node@v6\n  if: success() && failure()\n- run: ./bad.sh", "bad.sh"},
		{"contradictory chmod", "ubuntu-latest", "", "- run: chmod +x bad.sh\n  if: failure() && success()\n- run: ./bad.sh", "bad.sh"},
		{"negated contradictory status", "ubuntu-latest", "", "- run: ./bad.sh\n  if: success() && !success()", ""},
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
		{"padded checkout path", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {path: ' source '}\n- run: ./source/bad.sh", "bad.sh"},
		{"padded expression checkout path", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {path: \"${{ ' source ' }}\"}\n- run: ./source/bad.sh", "bad.sh"},
		{"whitespace checkout path", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {path: '  '}\n- run: ./bad.sh", "bad.sh"},
		{"checkout step Git worktree", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  env: {GIT_WORK_TREE: /tmp}\n- run: ./bad.sh", ""},
		{"checkout job Git directory", "ubuntu-latest", "env: {GIT_DIR: /tmp/git}", "- run: ./bad.sh", ""},
		{"checkout job Git config", "ubuntu-latest", "env: {GIT_CONFIG_COUNT: '1', GIT_CONFIG_KEY_0: core.worktree, GIT_CONFIG_VALUE_0: /tmp}", "- run: ./bad.sh", ""},
		{"checkout dynamic environment", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  env: ${{ fromJSON(inputs.env) }}\n- run: ./bad.sh", ""},
		{"checkout Git trace", "ubuntu-latest", "env: {GIT_TRACE: '1'}", "- run: ./bad.sh", "bad.sh"},
		{"checkout alternate server", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {github-server-url: 'https://git.example.com'}\n- run: ./bad.sh", ""},
		{"checkout unknown server", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {github-server-url: '${{ inputs.server }}'}\n- run: ./bad.sh", ""},
		{"checkout empty server", "ubuntu-latest", "", "- uses: actions/checkout@v6\n  with: {github-server-url: ''}\n- run: ./bad.sh", "bad.sh"},
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
				if !strings.Contains(strings.ToLower(line), strings.ToLower(filepath.Base(tc.want))) {
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
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		env  string
		want bool
	}{
		{"SHELLOPTS: nounset", false},
		{"PATH: /usr/bin", true},
		{"PATH: /usr/bin, SHELLOPTS: nounset", false},
		{"GIT_WORK_TREE: /tmp", false},
		{"GIT_INDEX_FILE: /tmp/index", false},
		{"GIT_TRACE: '1'", true},
	} {
		t.Run(tc.env, func(t *testing.T) {
			workflow := writeShellcheckFixture(t, root, ".github/workflows/test.yml", "on: push\nenv: {"+tc.env+"}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n      - run: ./bad.sh \"$UNSET\"\n")
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" }); found != tc.want {
				t.Fatalf("unexpected executable-bit result: %+v", result.Diagnostics)
			}
		})
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
		{"empty expression repository", "repository: ${{ '' }}", "", true},
		{"empty expression ref", "ref: ${{ '' }}", "", true},
		{"empty expression sparse checkout", "sparse-checkout: ${{ '' }}", "", true},
		{"null expression ref", "ref: ${{ null }}", "", true},
		{"whitespace expression ref", "ref: ${{ '  ' }}", "", true},
		{"whitespace literal ref", "ref: '  '", "", true},
		{"dynamic repository", "repository: ${{ inputs.repository }}", "", false},
		{"dynamic ref", "ref: ${{ inputs.ref }}", "", false},
		{"dynamic sparse checkout", "sparse-checkout: ${{ inputs.paths }}", "", false},
		{"nonempty expression ref", "ref: ${{ 'other' }}", "", false},
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
