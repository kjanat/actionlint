package typecheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

var errGitHubReferenceUnavailable = errors.New("runny reference unavailable")

// Check access once per test process, before starting any reference containers.
// A cached image needs no registry credentials. Otherwise Docker's credentials,
// rather than a particular GitHub username, determine access to the private image.
var githubReferenceImage = sync.OnceValues(func() (string, error) {
	image := os.Getenv("ACTIONLINT_REFERENCE_IMAGE")
	if image == "" {
		return "", nil
	}
	if !strings.Contains(image, "@sha256:") {
		return "", errors.New("pin ACTIONLINT_REFERENCE_IMAGE to the digest of the image being tested")
	}
	run := func(args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, bytes.TrimSpace(output))
		}
		return nil
	}
	if err := run("info", "--format", "{{.ServerVersion}}"); err != nil {
		return "", fmt.Errorf("%w: Docker is not available: %w", errGitHubReferenceUnavailable, err)
	}
	if err := run("image", "inspect", image); err == nil {
		return image, nil
	}
	if err := run("manifest", "inspect", image); err != nil {
		return "", fmt.Errorf("%w: image is not cached and Docker cannot access it; authenticate to its registry with an account that has access: %w", errGitHubReferenceUnavailable, err)
	}
	if err := run("pull", image); err != nil {
		return "", fmt.Errorf("pulling accessible reference image: %w", err)
	}
	return image, nil
})

// ACTIONLINT_REFERENCE_IMAGE also enables live reference checks elsewhere in
// this repository. Without it, the documentation tests run entirely offline.
func checkGitHubReferenceLive(t *testing.T, input string, variables map[string]any, want any) {
	t.Helper()
	checkGitHubReferenceOutcome(t, input, variables, githubReferenceExpectation{value: want})
}

type githubReferenceFailure uint8

const (
	githubReferenceSuccess githubReferenceFailure = iota
	githubReferenceParseFailure
	githubReferenceRuntimeFailure
)

type githubReferenceExpectation struct {
	failure       githubReferenceFailure
	value         any
	exceptionType string
}

// runGitHubReference invokes a command in the pinned image after checking access.
func runGitHubReference(t *testing.T, operation, input string, variables map[string]any) ([]byte, string) {
	t.Helper()
	image, err := githubReferenceImage()
	if errors.Is(err, errGitHubReferenceUnavailable) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if image == "" {
		return nil, ""
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	args := []string{"run", "--rm", "-i", "--pull=never"}
	if variables != nil {
		data, err := json.Marshal(variables)
		if err != nil {
			t.Fatal(err)
		}
		args = append(args, "-e", "RUNNY_CONTEXT="+string(data))
	}
	// The next runner implements the current reference, including case().
	args = append(args, image, operation, "--engine", "next", "--strict-json")
	command := exec.CommandContext(ctx, "docker", args...)
	command.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("Runny failed: %v\n%s\n%s", err, stderr.String(), output)
	}
	return output, image
}

// Check parse and evaluation separately: Runny reports both failures as a
// rejected result, while a skipped invalid expression can still return a value.
func checkGitHubReferenceOutcome(t *testing.T, input string, variables map[string]any, want githubReferenceExpectation) {
	t.Helper()
	output, image := runGitHubReference(t, "eval-expr", input, variables)
	if image == "" {
		return
	}
	var report struct {
		RunnerSHA string `json:"runnerSha"`
		Engines   []struct {
			Engine        string          `json:"engine"`
			Parsed        *bool           `json:"parsed"`
			Evaluated     *bool           `json:"evaluated"`
			Accepted      *bool           `json:"accepted"`
			Diagnosed     *bool           `json:"diagnosed"`
			Kind          string          `json:"kind"`
			Canonical     json.RawMessage `json:"canonical"`
			ExceptionType string          `json:"exceptionType"`
			Exception     string          `json:"exception"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("invalid Runny response: %v\n%s", err, output)
	}
	if report.RunnerSHA == "" || len(report.Engines) != 1 {
		t.Fatalf("missing reference identity or unexpected engine count: %s", output)
	}
	engine := report.Engines[0]
	if engine.Engine != "next" || engine.Parsed == nil || engine.Evaluated == nil || engine.Accepted == nil || engine.Diagnosed == nil {
		t.Fatalf("missing reference outcome fields: %s", output)
	}
	wantParsed := want.failure != githubReferenceParseFailure
	wantEvaluated := want.failure == githubReferenceSuccess
	if *engine.Parsed != wantParsed || *engine.Evaluated != wantEvaluated || *engine.Accepted != wantEvaluated || *engine.Diagnosed {
		t.Fatalf("unexpected reference outcome for failure phase %d: %s", want.failure, output)
	}
	if wantEvaluated {
		if engine.Kind == "" || engine.ExceptionType != "" || engine.Exception != "" {
			t.Fatalf("missing value kind or unexpected exception: %s", output)
		}
		var canonical any
		if len(engine.Canonical) != 0 {
			if err := json.Unmarshal(engine.Canonical, &canonical); err != nil {
				t.Fatalf("invalid Runny value: %v\n%s", err, output)
			}
		} else if engine.Kind != "Null" {
			t.Fatalf("missing Runny value: %s", output)
		}
		if diff := cmp.Diff(want.value, canonical); diff != "" {
			t.Fatalf("Runny value mismatch (-want +got):\n%s\n%s", diff, output)
		}
	} else if engine.ExceptionType != want.exceptionType || engine.Exception == "" || engine.Kind != "" || len(engine.Canonical) != 0 {
		t.Fatalf("unexpected reference failure details; want %q: %s", want.exceptionType, output)
	}
	t.Logf("verified with %s, runner %s", image, report.RunnerSHA)
}

func checkGitHubWorkflowReference(t *testing.T, workflow string, wantAccepted bool) {
	t.Helper()
	output, image := runGitHubReference(t, "parse-workflow", workflow, nil)
	if image == "" {
		return
	}
	var report struct {
		RunnerSHA string `json:"runnerSha"`
		Engines   []struct {
			Engine         string            `json:"engine"`
			Accepted       *bool             `json:"accepted"`
			TemplateErrors []json.RawMessage `json:"templateErrors"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("invalid Runny workflow response: %v\n%s", err, output)
	}
	if report.RunnerSHA == "" || len(report.Engines) != 1 || report.Engines[0].Engine != "next" || report.Engines[0].Accepted == nil {
		t.Fatalf("missing workflow reference identity or outcome: %s", output)
	}
	engine := report.Engines[0]
	if *engine.Accepted != wantAccepted || (len(engine.TemplateErrors) == 0) != wantAccepted {
		t.Fatalf("Runny workflow acceptance mismatch, want accepted=%t: %s", wantAccepted, output)
	}
	t.Logf("verified workflow with %s, runner %s", image, report.RunnerSHA)
}
