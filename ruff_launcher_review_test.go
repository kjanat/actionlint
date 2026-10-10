package actionlint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestRuffLauncherArguments(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(1)\n"
	for _, launcher := range []string{"env", "uvx", "uvx-pinned"} {
		tool := filepath.Join(t.TempDir(), strings.TrimSuffix(launcher, "-pinned"))
		if runtime.GOOS == "windows" {
			tool += ".exe"
		}
		if err := os.WriteFile(tool, data, 0o700); err != nil {
			t.Fatal(err)
		}
		var prefix []string
		switch launcher {
		case "env":
			prefix = []string{"NAME=--silent", "ruff"}
		case "uvx-pinned":
			prefix = []string{"ruff@0.17.0"}
		default:
			prefix = []string{"--from", "ruff", "--no-cache", "ruff"}
		}
		encoded, err := json.Marshal(prefix)
		if err != nil {
			t.Fatal(err)
		}
		for _, optional := range []bool{false, true} {
			for _, disabled := range []string{"", "shell", "config"} {
				calls, output := filepath.Join(t.TempDir(), "calls"), filepath.Join(t.TempDir(), "output.json")
				args := []string{"--from", "ruff"}
				if launcher == "env" {
					args = []string{"RUFF_OUTPUT_FILE=" + output, "ruff"}
				}
				options := &ExternalCommandOptions{Executable: &tool, Optional: optional, Arguments: args, Environment: []string{"ACTIONLINT_TEST_RUFF=1", "ACTIONLINT_TEST_RUFF_PREFIX=" + string(encoded), "ACTIONLINT_TEST_RUFF_CALLS=" + calls}}
				input := source
				var config *Config
				switch disabled {
				case "shell":
					input = strings.Replace(source, "shell: python", "shell: bash", 1)
				case "config":
					config, err = ParseConfig([]byte("tools: {ruff: false}"))
					if err != nil {
						t.Fatal(err)
					}
				}
				_, err := Analyze(t.Context(), AnalysisRequest{RuffOptions: options, Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(input), Config: config}}})
				if (err != nil) != (disabled == "") {
					t.Fatalf("invalid launcher result: optional=%v disabled=%s error=%v", optional, disabled, err)
				}
				for _, path := range []string{calls, output} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("invalid launcher executed or wrote output: %s, %v", path, err)
					}
				}
			}
		}
		for _, literal := range []bool{false, true} {
			for _, flags := range [][]string{{"--preview"}, {"other.py"}, {"--select", "F", "other.py"}, {"--silent"}, {"--extension=py:ipynb"}, {"--output-file", "output.json"}, {"--isolated"}} {
				t.Run(launcher+strconv.FormatBool(literal)+strings.Join(flags, " "), func(t *testing.T) {
					calls := filepath.Join(t.TempDir(), "calls")
					options := &ExternalCommandOptions{Environment: []string{"ACTIONLINT_TEST_RUFF=1", "ACTIONLINT_TEST_RUFF_OUTPUT=[]", "ACTIONLINT_TEST_RUFF_PREFIX=" + string(encoded), "ACTIONLINT_TEST_RUFF_CALLS=" + calls}}
					command := strconv.Quote(tool)
					if literal {
						options.Executable = &tool
						options.Arguments = append(append([]string(nil), prefix...), flags...)
					} else {
						words := []string{command}
						for _, arg := range prefix {
							words = append(words, strconv.Quote(arg))
						}
						command = strings.Join(words, " ")
						options.Arguments = flags
					}
					options.Optional = true
					_, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, RuffOptions: options, Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source)}}})
					content, readErr := os.ReadFile(calls)
					if flags[0] != "--preview" {
						if err == nil || !os.IsNotExist(readErr) {
							t.Fatalf("invalid checker flags reached launcher: %v, %v, %s", err, readErr, content)
						}
						return
					}
					if err != nil || readErr != nil || string(content) != strings.Join(prefix, " ")+" --version\n"+strings.Join(prefix, " ")+" check --preview --isolated --target-version py314 --select F --ignore-noqa --no-fix --no-cache --output-format json --stdin-filename actionlint.py --extension py:python -\n" {
						t.Fatalf("launcher invocation changed: %v, %v, %s", err, readErr, content)
					}
				})
			}
		}
	}
}
