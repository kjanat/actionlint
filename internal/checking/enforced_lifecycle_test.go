package checking

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPrepareEnforcedLifecycleLoadsOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const base = `lint:
  lifecycle:
    policies:
      - id: base
        description: Required runner support
        match: {subjects: [runners], events: [support-ends]}
        notifications: [{starts: {on: effective-date}, severity: error}]
`
	paths := map[string]string{filepath.Join(dir, "base.yml"): base, filepath.Join(dir, "policy.yml"): "extends: [base.yml]\n"}
	reads := 0
	options := Options{WorkingDir: dir, EnforcePolicies: []string{"policy.yml"}, ReadFile: func(path string) ([]byte, error) {
		reads++
		content, ok := paths[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(content), nil
	}}
	prepared, err := prepareEnforcedLifecycle(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	if reads != 2 || len(prepared.EnforcedLifecycle) != 1 || len(prepared.EnforcePolicies) != 0 || !slices.Contains(prepared.enforcedPolicyInputs, filepath.Join(dir, "base.yml")) || !slices.Contains(prepared.enforcedPolicyInputs, filepath.Join(dir, "policy.yml")) {
		t.Fatalf("reads=%d, prepared=%+v", reads, prepared)
	}
	prepared, err = prepareEnforcedLifecycle(context.Background(), prepared)
	if err != nil || reads != 2 || len(prepared.EnforcedLifecycle) != 1 {
		t.Fatalf("second load: reads=%d, error=%v", reads, err)
	}
}

func TestRequiredPolicyErrorPreservesCause(t *testing.T) {
	t.Parallel()
	_, err := prepareEnforcedLifecycle(t.Context(), Options{EnforcePolicies: []string{"missing.yml"}, ReadFile: func(string) ([]byte, error) { return nil, os.ErrNotExist }})
	var required *RequiredPolicyError
	if !errors.As(err, &required) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("required policy error did not preserve its cause: %v", err)
	}
}
