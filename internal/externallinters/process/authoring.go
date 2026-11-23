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

	"actionlint.kjanat.dev/v2/internal/externallinters/integrationconfig"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation/shell"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

type AuthoringResult struct {
	Source      string                  `json:"source"`
	Preparation *preparation.SourcePlan `json:"preparation,omitempty"`
	Config      json.RawMessage         `json:"config"`
	Variables   map[string]string       `json:"variables"`
	Result      *DecodedCheck           `json:"result,omitempty"`
}

type AuthoringTester struct {
	manifest      toolmanifest.Manifest
	backend       *Backend
	configuration *integrationconfig.SettingsResolver
	preparation   *preparation.SourcePlanner
}

// ValidateManifest compiles every jq program and schema in a process manifest
// without probing or invoking its entry.
func ValidateManifest(manifest toolmanifest.Manifest, programs toolmanifest.Programs) error {
	_, err := NewAuthoringTester(manifest, programs)
	return err
}

func NewAuthoringTester(manifest toolmanifest.Manifest, programs toolmanifest.Programs) (*AuthoringTester, error) {
	backend, err := authoringBackend(manifest, programs)
	if err != nil {
		return nil, err
	}
	settingsResolver, err := integrationconfig.New(manifest, programs)
	if err != nil {
		return nil, err
	}
	sourcePlanner, err := preparation.NewSourcePlanner(manifest.PreparationTransform, programs)
	if err != nil {
		return nil, err
	}
	return &AuthoringTester{manifest: manifest, backend: backend, configuration: settingsResolver, preparation: sourcePlanner}, nil
}

// EvaluateFixture runs no child process; the timeout bounds jq evaluation.
func EvaluateFixture(ctx context.Context, manifest toolmanifest.Manifest, programs toolmanifest.Programs, request scriptengine.Request, stdout *string, variables map[string]string, exitCode *int) (AuthoringResult, error) {
	tester, err := NewAuthoringTester(manifest, programs)
	if err != nil {
		return AuthoringResult{}, err
	}
	return tester.Evaluate(ctx, request, stdout, variables, exitCode, nil)
}

// Evaluate runs no child process; ctx bounds jq evaluation.
func (t *AuthoringTester) Evaluate(ctx context.Context, request scriptengine.Request, stdout *string, variables map[string]string, exitCode *int, facts *preparation.SourcePlanInput) (AuthoringResult, error) {
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
	var plan *preparation.SourcePlan
	if manifest.ConfigurationTransform != nil || manifest.PreparationTransform != nil || len(manifest.ConfigurationPaths) != 0 {
		settings, err := t.configuration.Resolve(ctx, request.Config, "", nil)
		if err != nil {
			return AuthoringResult{}, err
		}
		request.Config = settings.Options
		if manifest.PreparationTransform != nil && facts == nil {
			return AuthoringResult{}, errors.New("fixture needs preparation facts for the preparation transform")
		}
		context := preparation.SourcePlanInput{}
		if facts != nil {
			context = *facts
		}
		context.Source, context.Settings = request.Source, settings
		prepared, err := t.preparation.Plan(ctx, context)
		if err != nil {
			return AuthoringResult{}, err
		}
		plan = &prepared
		if prepared.Skip != "" {
			return AuthoringResult{Source: request.Source, Preparation: plan, Config: request.Config, Variables: values}, nil
		}
		request.Source = shell.Prepare(request.Source, prepared.Prefix, prepared.Startup).Text()
		request.Config = prepared.Config
		maps.Copy(values, prepared.Variables)
	}

	config, err := decodeConfig(request.Config)
	if err != nil {
		return AuthoringResult{}, fmt.Errorf("tools.%s.config: %w", backend.name, err)
	}
	if err := backend.ValidateConfig(ctx, request.Config); err != nil {
		return AuthoringResult{}, err
	}
	engine := engineConfig{value: config}
	if backend.configProgram != nil {
		engine, err = backend.configProgram.apply(ctx, config, values)
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
	out := AuthoringResult{Source: request.Source, Preparation: plan, Config: encoded, Variables: values}
	if stdout == nil || plan != nil && plan.Skip != "" {
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
	if manifest.Role == toolmanifest.RoleDocument {
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

func authoringBackend(manifest toolmanifest.Manifest, programs toolmanifest.Programs) (*Backend, error) {
	if manifest.Transport != toolmanifest.TransportProcess || manifest.Process == nil {
		return nil, fmt.Errorf("tool provider %s must use the process transport", manifest.ID)
	}
	// Authoring has no child environment, so omit environment passthrough values.
	spec := *manifest.Process
	spec.EnvPassthrough = nil
	manifest.Process = &spec
	return NewBackend(manifest, &hostprocess.Command{Exe: spec.Entry}, Options{Programs: programs})
}

const AuthoringTimeout = time.Second
