package commands

import "testing"

func TestWorkflowCommandParser(t *testing.T) {
	t.Parallel()
	// Compared with pinned ActionCommand.TryParseV2 in
	// .review-artifacts/pr343-script-rules/runner-reference.
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"::notice:Up to date", true},
		{"::error:msg", true},
		{"::warning single-colon", true},
		{"::group unterminated", true},
		{"::notic::msg", true},
		{"::notice::Up to date", false},
		{"::NOTICE::ok", false},
		{"  ::warning file=a,line=2::msg", false},
		{"::group::", false},
		{"::endgroup::", false},
		{"[System.IO.Path]::GetTempPath()", false},
		{"example ::notic::msg", false},
		{"::debug ignored-property::ok", false},
		{"::add-matcher::matcher.json", false},
		{"::internal-set-repo-path::path", false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			var state WorkflowCommandState
			if got := state.WorkflowCommandProblem(tc.text); (got != "") != tc.want {
				t.Fatalf("got %q, want finding %v", got, tc.want)
			}
		})
	}
}

func TestWorkflowStopAndResume(t *testing.T) {
	t.Parallel()
	var state WorkflowCommandState
	for _, text := range []string{"::stop-commands::secret-token", "::notic::example", "::notice:example", "::secret-token::", "::notice::real"} {
		if got := state.WorkflowCommandProblem(text); got != "" {
			t.Fatalf("%q: %s", text, got)
		}
	}
	if state.WorkflowCommandProblem("::notice:broken") == "" {
		t.Fatal("processing did not resume")
	}
}
