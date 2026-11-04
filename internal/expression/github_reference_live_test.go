package expression

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

var errGitHubReferenceUnavailable = errors.New("Runny reference unavailable")

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
		return "", fmt.Errorf("%w: Docker is not available: %v", errGitHubReferenceUnavailable, err)
	}
	if err := run("image", "inspect", image); err == nil {
		return image, nil
	}
	if err := run("manifest", "inspect", image); err != nil {
		return "", fmt.Errorf("%w: image is not cached and Docker cannot access it; authenticate to its registry with an account that has access: %v", errGitHubReferenceUnavailable, err)
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
	image, err := githubReferenceImage()
	if errors.Is(err, errGitHubReferenceUnavailable) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if image == "" {
		return
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
	args = append(args, image, "eval-expr", "--engine", "next", "--strict-json")
	command := exec.CommandContext(ctx, "docker", args...)
	command.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("Runny failed: %v\n%s\n%s", err, stderr.String(), output)
	}
	var report struct {
		RunnerSHA string `json:"runnerSha"`
		Engines   []struct {
			Engine    string `json:"engine"`
			Parsed    bool   `json:"parsed"`
			Evaluated bool   `json:"evaluated"`
			Accepted  bool   `json:"accepted"`
			Diagnosed bool   `json:"diagnosed"`
			Canonical any    `json:"canonical"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("invalid Runny response: %v\n%s", err, output)
	}
	if report.RunnerSHA == "" || len(report.Engines) != 1 {
		t.Fatalf("missing reference identity or unexpected engine count: %s", output)
	}
	engine := report.Engines[0]
	if engine.Engine != "next" || !engine.Parsed || !engine.Evaluated || !engine.Accepted || engine.Diagnosed {
		t.Fatalf("reference did not accept and evaluate the expression: %s", output)
	}
	if diff := cmp.Diff(want, engine.Canonical); diff != "" {
		t.Fatalf("Runny value mismatch (-want +got):\n%s\n%s", diff, output)
	}
	t.Logf("verified with %s, runner %s", image, report.RunnerSHA)
}
