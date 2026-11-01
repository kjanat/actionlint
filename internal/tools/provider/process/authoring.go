package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// AuthoringResult is the host's evaluation of one fixture stdout. It never
// invokes the manifest entry.
type AuthoringResult struct {
	Config    json.RawMessage   `json:"config"`
	Variables map[string]string `json:"variables"`
	Result    *Result           `json:"result,omitempty"`
}

// AuthoringTester holds one compiled process pipeline for a fixture suite.
// It does not retain per-fixture configuration or output.
type AuthoringTester struct {
	manifest provider.Manifest
	backend  *Backend
}

// ValidateManifest compiles every jq program and schema in a process manifest
// without probing or invoking its entry.
func ValidateManifest(manifest provider.Manifest, programs provider.Programs) error {
	_, err := NewAuthoringTester(manifest, programs)
	return err
}

// NewAuthoringTester compiles a manifest once for fixture evaluation.
func NewAuthoringTester(manifest provider.Manifest, programs provider.Programs) (*AuthoringTester, error) {
	backend, err := authoringBackend(manifest, programs)
	if err != nil {
		return nil, err
	}
	return &AuthoringTester{manifest: manifest, backend: backend}, nil
}

// EvaluateFixture applies the exact config and stdout pipeline used by a
// process backend. The timeout bounds jq evaluation; no child process runs.
func EvaluateFixture(ctx context.Context, manifest provider.Manifest, programs provider.Programs, request analyzer.ScriptRequest, stdout *string, variables map[string]string, exitCode *int) (AuthoringResult, error) {
	tester, err := NewAuthoringTester(manifest, programs)
	if err != nil {
		return AuthoringResult{}, err
	}
	return tester.Evaluate(ctx, request, stdout, variables, exitCode)
}

// Evaluate applies the compiled pipeline to one fixture. The per-case
// context bounds jq evaluation; no child process runs.
func (t *AuthoringTester) Evaluate(ctx context.Context, request analyzer.ScriptRequest, stdout *string, variables map[string]string, exitCode *int) (AuthoringResult, error) {
	if err := ctx.Err(); err != nil {
		return AuthoringResult{}, err
	}
	manifest, backend := t.manifest, t.backend
	var err error
	directory, err := backend.directory(request.Directory)
	if err != nil {
		return AuthoringResult{}, err
	}
	values := maps.Clone(variables)
	if values == nil {
		values = map[string]string{}
	}
	values["filename"] = request.Filename
	values["filename_abs"] = request.Filename
	if !filepath.IsAbs(request.Filename) {
		values["filename_abs"] = filepath.Join(directory, request.Filename)
	}
	config, err := decodeConfig(request.Config)
	if err != nil {
		return AuthoringResult{}, fmt.Errorf("tools.%s.config: %w", backend.name, err)
	}
	if err := backend.ValidateConfig(ctx, request.Config); err != nil {
		return AuthoringResult{}, err
	}
	engine := engineConfig{value: config}
	if backend.transform != nil {
		engine, err = backend.transform.apply(ctx, config, values)
		if err != nil {
			return AuthoringResult{}, fmt.Errorf("tools.%s.config: %w", backend.name, err)
		}
	}
	for name, value := range engine.vars {
		if value == nil {
			delete(values, name)
		} else {
			values[name] = *value
		}
	}
	if err := checkForbidden(backend.name, engine.value, backend.spec.Config.Forbidden); err != nil {
		return AuthoringResult{}, err
	}
	encoded, err := json.Marshal(engine.value)
	if err != nil {
		return AuthoringResult{}, err
	}
	out := AuthoringResult{Config: encoded, Variables: values}
	if stdout == nil {
		return out, nil
	}
	if len(*stdout) > MaxOutputBytes {
		return out, fmt.Errorf("tool output exceeds %d bytes", MaxOutputBytes)
	}
	code := 0
	if exitCode != nil {
		code = *exitCode
	}
	accepted := backend.spec.ExitCodes
	if len(accepted) == 0 {
		accepted = []int{0}
	}
	if !slices.Contains(accepted, code) {
		return out, fmt.Errorf("tool exited with code %d", code)
	}
	values["exit_code"] = strconv.Itoa(code)
	result, err := backend.decoder.Decode(ctx, []byte(*stdout), values)
	if err != nil {
		return out, fmt.Errorf("tools.%s: %w", backend.name, err)
	}
	result.Directory = directory
	if manifest.Role == provider.RoleDocument {
		result.Diagnostics = documentFindings(result.Diagnostics, request, directory)
	}
	if err := backend.review(&result, request); err != nil {
		return out, err
	}
	out.Result = &result
	var failures error
	for _, message := range result.Errors {
		failures = errors.Join(failures, errors.New(message))
	}
	return out, failures
}

func authoringBackend(manifest provider.Manifest, programs provider.Programs) (*Backend, error) {
	if manifest.Transport != provider.TransportProcess || manifest.Process == nil {
		return nil, fmt.Errorf("tool provider %s must use the process transport", manifest.ID)
	}
	// NewBackend compiles the manifest and checks argument structure, but its
	// normal setup also snapshots environment passthrough values. Authoring has
	// no child environment, so omit those values and keep construction inert.
	spec := *manifest.Process
	spec.EnvPassthrough = nil
	manifest.Process = &spec
	return NewBackend(manifest, &analyzerhost.Command{Exe: spec.Entry}, Options{Programs: programs})
}

// AuthoringTimeout is the per-case jq evaluation limit.
const AuthoringTimeout = time.Second
