package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results/edits"
)

func TestFixGitHubContextEnvironment(t *testing.T) {
	t.Parallel()
	for _, scalar := range []string{
		`bun "${{ github.action_path }}/../runtime/client.mjs" begin`,
		`'bun "${{ github.action_path }}/../runtime/client.mjs" begin'`,
		`"bun \"${{ github.action_path }}/../runtime/client.mjs\" begin"`,
		`"bun \"${{ \u0067ithub.action_path }}/../runtime/client.mjs\" begin"`,
		"|-\n        bun \"${{ github.action_path }}/../runtime/client.mjs\" begin # comment",
		">-\n        bun \"${{ github.action_path }}/../runtime/client.mjs\"\n        begin",
		`echo "é ${{ github['action_path'] }}suffix ${{ runner.temp }}"`,
		`echo "${{ github.head_ref }}"`,
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(scalar+ending, func(t *testing.T) {
				t.Parallel()
				path := filepath.Join(t.TempDir(), "action.yml")
				before := strings.ReplaceAll("name: example\ndescription: example\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: "+scalar+"\n", "\n", ending)
				after := strings.NewReplacer(
					"${{ github.action_path }}", "${GITHUB_ACTION_PATH}",
					"${{ \\u0067ithub.action_path }}", "${GITHUB_ACTION_PATH}",
					"${{ github['action_path'] }}", "${GITHUB_ACTION_PATH}",
					"${{ runner.temp }}", "${RUNNER_TEMP}",
					"${{ github.head_ref }}", "${GITHUB_HEAD_REF}",
				).Replace(before)
				if err := os.WriteFile(path, []byte(before), 0600); err != nil {
					t.Fatal(err)
				}
				args := []string{"fix", "--norc", "--include-suggested", path}
				got := testRunCommand("", args...)
				var plan edits.Plan
				if err := json.Unmarshal([]byte(got.Stdout), &plan); err != nil {
					t.Fatalf("preview: %+v: %v", got, err)
				}
				if got.Status != 0 || len(plan.Changes) != 1 || plan.Changes[0].After != after {
					t.Fatalf("incorrect fix: %+v; plan=%+v; want=%q", got, plan, after)
				}
				got = testRunCommand("", append(args, "--apply", "--quiet")...)
				written, err := os.ReadFile(path)
				if got.Status != 0 || err != nil || string(written) != after {
					t.Fatalf("fix did not apply and recheck: %+v; written=%q; error=%v", got, written, err)
				}
			})
		}
	}
}

func TestFixEnvironmentAvailabilityPerFinding(t *testing.T) {
	t.Parallel()
	const content = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: bash\n        run: |\n          echo \"${{ github.workspace }}\"\n          echo '${{ github.workspace }}'\npermissions: {}\n"
	path := filepath.Join(t.TempDir(), "workflow.yml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	for _, include := range []bool{false, true} {
		args := []string{"fix", "--norc", path}
		if include {
			args = append(args, "--include-suggested")
		}
		got := testRunCommand("", args...)
		var plan edits.Plan
		if err := json.Unmarshal([]byte(got.Stdout), &plan); err != nil || got.Status != 0 {
			t.Fatalf("preview: %+v: %v", got, err)
		}
		if !include {
			if len(plan.Changes) != 0 {
				t.Fatalf("review-required edit applied automatically: %+v", plan)
			}
			continue
		}
		want := strings.Replace(content, "${{ github.workspace }}", "${GITHUB_WORKSPACE}", 1)
		if len(plan.Changes) != 1 || plan.Changes[0].After != want {
			t.Fatalf("fixability was not decided per occurrence: %+v", plan)
		}
	}
}
