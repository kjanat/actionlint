package actionlint

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestActionDiscoverySymlinkRoot(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(strconv.FormatBool(mixed), func(t *testing.T) {
			root := t.TempDir()
			writeShellcheckFixture(t, root, ".git", "gitdir: elsewhere\n")
			writeShellcheckFixture(t, root, "action.yml", standaloneComposite)
			want := 1
			if mixed {
				writeShellcheckFixture(t, root, ".github/workflows/ci.yml", commandGoodWorkflow)
				want++
			}
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(root, alias); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: alias})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Repository("")
			if err != nil || len(result.Documents) != want || len(result.Diagnostics) != 0 {
				t.Fatalf("want %d clean documents: result=%+v error=%v", want, result, err)
			}
		})
	}
}

func TestActionDiscoveryDoesNotHideWorkflowTypo(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, ".git", "gitdir: elsewhere\n")
	writeShellcheckFixture(t, root, "action.yml", standaloneComposite)
	writeShellcheckFixture(t, root, ".github/workflow/ci.yml", commandGoodWorkflow)
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Repository("")
	if err == nil || !strings.Contains(err.Error(), "looks misspelled") {
		t.Fatalf("expected workflow typo despite action discovery, got %v", err)
	}
}

func TestExplicitActionSnapshotAndDeduplication(t *testing.T) {
	for _, disk := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			for _, runner := range []string{"ubuntu-latest", "windows-latest"} {
				t.Run(fmt.Sprintf("disk=%t/reverse=%t/%s", disk, reverse, runner), func(t *testing.T) {
					root := t.TempDir()
					if disk {
						writeShellcheckFixture(t, root, "inner/action.yml", standaloneComposite)
					}
					manifest := "name: inner\ndescription: inner\nruns:\n  using: node24\n"
					workflow := "on: push\njobs:\n  test:\n    runs-on: " + runner + "\n    steps:\n      - uses: ./inner\n        with: {unknown: value}\n"
					project := &Project{root: root}
					sources := []SourceUnit{
						{Path: "ci.yml", Content: []byte(workflow), Project: project},
						{Path: "inner/action.yml", Content: []byte(manifest), Project: project},
					}
					if reverse {
						sources[0], sources[1] = sources[1], sources[0]
					}
					result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: sources})
					if err != nil {
						t.Fatal(err)
					}
					missingMain, unknownInput := 0, 0
					for _, d := range result.Diagnostics {
						switch {
						case strings.Contains(d.Message, "main") && strings.Contains(d.Message, "required"):
							missingMain++
							if d.Path != filepath.Join("inner", "action.yml") {
								t.Errorf("metadata finding should use declaration, got %+v", d)
							}
						case strings.Contains(d.Message, "unknown"):
							unknownInput++
						default:
							t.Errorf("unexpected diagnostic %+v", d)
						}
					}
					if missingMain != 1 || unknownInput != 1 {
						t.Fatalf("want one declaration error and one caller input error; main=%d input=%d diagnostics=%+v", missingMain, unknownInput, result.Diagnostics)
					}
				})
			}
		}
	}
}
