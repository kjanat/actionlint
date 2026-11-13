package selection_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	ruleexec "actionlint.kjanat.dev/v2/internal/checking/executor/registration"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func TestExecutableIndexPreparation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, level string
		failure     error
		unavailable bool
	}{
		{name: "available", level: "on"},
		{name: "disabled", level: "off"},
		{name: "missing index", level: "on", failure: fs.ErrNotExist, unavailable: true},
		{name: "access denied", level: "on", failure: fs.ErrPermission},
		{name: "cancellation", level: "on", failure: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			indexPath := filepath.Join(root, ".git", "index")
			rules := checking.ConfigurationRules()
			config, err := rules.Parse([]byte("lint: {rules: {correctness: {executable-bit: " + tc.level + "}}}"))
			if err != nil {
				t.Fatal(err)
			}
			loads := 0
			var recorded []string
			snapshot := execution.NewIndexSnapshot(indexPath, map[string]string{"script.sh": "100644"}, tc.failure)
			input := ruleexec.Inputs{
				Rules: rules, Config: config, Context: t.Context(), ProjectRoot: root,
				Reader: files.New(t.Context(), nil, func(path string) { recorded = append(recorded, path) }),
				Index: func(_ context.Context, path string) *execution.IndexSnapshot {
					if path != root {
						t.Fatalf("loaded wrong repository: %s", path)
					}
					loads++
					return snapshot
				},
			}
			registrations := ruleexec.Registrations(rules)
			index := slices.IndexFunc(registrations, func(r ruleexec.Registration) bool { return r.Name == "executable-bit" })
			if index < 0 || registrations[index].Prepare == nil {
				t.Fatal("executable-bit has no preparation")
			}
			outcome, err := registrations[index].Prepare(input)
			if tc.failure != nil && !tc.unavailable {
				if !errors.Is(err, tc.failure) {
					t.Fatalf("operation failure changed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.unavailable {
				unavailable, ok := outcome.(ruleexec.UnavailableInputs)
				if !ok || unavailable.Reason == "" || len(recorded) != 0 {
					t.Fatalf("missing index became usable: %+v", outcome)
				}
				return
			}
			prepared, ok := outcome.(ruleexec.PreparedInputs)
			if !ok {
				t.Fatalf("inputs not supplied: %T", outcome)
			}
			if tc.level == "off" {
				if loads != 0 || len(recorded) != 0 {
					t.Fatal("disabled rule read the index")
				}
				return
			}
			if loads != 1 || !slices.Equal(recorded, []string{indexPath}) || prepared.Inputs.ExecutableIndex != snapshot || input.ExecutableIndex != nil {
				t.Fatalf("preparation lost snapshot ownership: loads=%d recorded=%v", loads, recorded)
			}
			registrations[index].Build(prepared.Inputs)
			if loads != 1 {
				t.Fatal("constructor loaded the index again")
			}
		})
	}
}
