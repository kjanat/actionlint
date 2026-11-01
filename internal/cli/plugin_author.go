package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/process"
	"github.com/spf13/cobra"
)

const (
	authoringInputLimit    = 16 << 20
	authoringManifestLimit = 1 << 20
	authoringProgramLimit  = 16 << 20
)

type pluginValidateRequest struct {
	Path string
	JSON bool
}

type pluginTestRequest struct {
	Paths []string
	JSON  bool
}

type authoringReport struct {
	SchemaVersion int                    `json:"schema_version"`
	Valid         bool                   `json:"valid"`
	Error         string                 `json:"error,omitempty"`
	Cases         *[]authoringCaseResult `json:"cases,omitempty"`
}

type authoringFixture struct {
	Name         string                 `json:"name"`
	Request      analyzer.ScriptRequest `json:"request"`
	Stdout       *string                `json:"stdout,omitempty"`
	Variables    map[string]string      `json:"variables,omitempty"`
	ExitCode     *int                   `json:"exit_code,omitempty"`
	Expect       *process.Result        `json:"expect,omitempty"`
	ExpectConfig json.RawMessage        `json:"expect_config,omitempty"`
	ExpectVars   map[string]string      `json:"expect_vars,omitempty"`
	ExpectError  string                 `json:"expect_error,omitempty"`
}

type authoringCaseResult struct {
	Name      string            `json:"name"`
	Passed    bool              `json:"passed"`
	Config    json.RawMessage   `json:"config"`
	Variables map[string]string `json:"variables"`
	Result    *process.Result   `json:"result,omitempty"`
	Error     string            `json:"error,omitempty"`
}

func (a *commandApp) pluginAuthoringCommands() []*cobra.Command {
	validate := &cobra.Command{
		Use: "validate [manifest]", Short: "Validate a process provider manifest without running it",
		Long: "Validate a process provider manifest and compile its jq transforms without running the declared tool.\n\nExamples:\n  actionlint plugin validate\n  actionlint plugin validate ./actionlint-provider.json --json\n  actionlint plugin validate - < manifest.json",
		Args: cobra.MaximumNArgs(1), RunE: a.capture(operationPluginValidate),
	}
	validate.Flags().BoolVar(&a.inv.JSON, "json", false, "Write a JSON report")
	test := &cobra.Command{
		Use: "test [manifest fixtures]", Short: "Test process provider transforms against fixtures without running the tool",
		Long: "Run manifest jq transforms over fixture stdout and configuration. The declared tool is never run.\n\nExamples:\n  actionlint plugin test ./actionlint-provider.json ./fixtures.json --json\n  actionlint plugin test --json < test-input.json",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 && len(args) != 2 {
				return errors.New("accepts either no arguments or exactly two paths")
			}
			return nil
		}, RunE: a.capture(operationPluginTest),
	}
	test.Flags().BoolVar(&a.inv.JSON, "json", false, "Write a JSON report")
	return []*cobra.Command{validate, test}
}

func (r pluginValidateRequest) run(ctx context.Context, streams Command) (int, error) {
	manifest, programs, err := loadAuthorManifest(ctx, streams, r.Path)
	if err == nil {
		err = process.ValidateManifest(manifest, programs)
	}
	report := authoringReport{SchemaVersion: 1, Valid: err == nil}
	if err != nil {
		report.Error = err.Error()
	}
	if r.JSON {
		if writeErr := writeCommandJSON(streams.Stdout, report); writeErr != nil {
			return 1, writeErr
		}
	} else if !report.Valid {
		if _, writeErr := fmt.Fprintln(streams.Stdout, "Invalid: "+report.Error); writeErr != nil {
			return 1, writeErr
		}
	} else if _, writeErr := fmt.Fprintln(streams.Stdout, "Valid process provider manifest."); writeErr != nil {
		return 1, writeErr
	}
	if !report.Valid {
		return 1, nil
	}
	return 0, nil
}

func (r pluginTestRequest) run(ctx context.Context, streams Command) (int, error) {
	manifest, programs, fixtures, err := loadAuthorTests(ctx, streams, r.Paths)
	cases := []authoringCaseResult{}
	report := authoringReport{SchemaVersion: 1, Valid: true, Cases: &cases}
	if err != nil {
		report.Valid, report.Error = false, err.Error()
	} else {
		tester, compileErr := process.NewAuthoringTester(manifest, programs)
		if compileErr != nil {
			report.Valid, report.Error = false, compileErr.Error()
		}
		if len(fixtures) == 0 || len(fixtures) > 128 {
			report.Valid = false
			report.Error = "plugin test requires 1 to 128 cases"
		} else if compileErr == nil {
			seen := map[string]bool{}
			for _, fixture := range fixtures {
				caseResult := runAuthoringCase(ctx, tester, fixture, seen)
				*report.Cases = append(*report.Cases, caseResult)
				if !caseResult.Passed {
					report.Valid = false
				}
			}
		}
	}
	if err := writeCommandJSON(streams.Stdout, report); err != nil {
		return 1, err
	}
	if !report.Valid {
		return 1, nil
	}
	return 0, nil
}

func loadAuthorManifest(ctx context.Context, streams Command, path string) (provider.Manifest, provider.Programs, error) {
	if path == "-" {
		data, err := readBounded(streams.Stdin, authoringManifestLimit)
		if err != nil {
			return provider.Manifest{}, nil, err
		}
		manifest, err := provider.Parse(data)
		if err != nil {
			return provider.Manifest{}, nil, err
		}
		return manifest, nil, nil
	}
	if path == "" {
		path = "actionlint-provider.json"
	}
	reader := boundedAuthoringReader(ctx, path)
	return provider.ReadLocal(reader, path)
}

func boundedAuthoringReader(ctx context.Context, manifestPath string) *fsread.Reader {
	opener := fsread.New(ctx, nil, nil)
	remaining := authoringInputLimit
	return fsread.New(ctx, func(path string) ([]byte, error) {
		limit := authoringProgramLimit
		if filepath.Clean(path) == filepath.Clean(manifestPath) {
			limit = authoringManifestLimit
		}
		limit = min(limit, remaining)
		data, err := readReaderBounded(opener, path, limit)
		if err != nil {
			return nil, err
		}
		remaining -= len(data)
		return data, nil
	}, nil)
}

func readReaderBounded(reader *fsread.Reader, path string, limit int) ([]byte, error) {
	file, err := reader.Open(path)
	if err != nil {
		return nil, err
	}
	data, readErr := readBounded(file, limit)
	closeErr := file.Close()
	return data, errors.Join(readErr, closeErr)
}

func loadAuthorTests(ctx context.Context, streams Command, paths []string) (provider.Manifest, provider.Programs, []authoringFixture, error) {
	if len(paths) == 2 {
		manifest, programs, err := loadAuthorManifest(ctx, streams, paths[0])
		if err != nil {
			return provider.Manifest{}, nil, nil, err
		}
		file, err := os.Open(paths[1])
		if err != nil {
			return provider.Manifest{}, nil, nil, err
		}
		data, readErr := readBounded(file, authoringInputLimit)
		closeErr := file.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return provider.Manifest{}, nil, nil, err
		}
		fixtures, err := decodeFixtures(data)
		return manifest, programs, fixtures, err
	}
	data, err := readBounded(streams.Stdin, authoringInputLimit)
	if err != nil {
		return provider.Manifest{}, nil, nil, err
	}
	var input struct {
		Manifest json.RawMessage    `json:"manifest"`
		Cases    []authoringFixture `json:"cases"`
	}
	if err := decodeStrict(data, &input); err != nil {
		return provider.Manifest{}, nil, nil, fmt.Errorf("invalid plugin test input: %w", err)
	}
	if len(input.Manifest) == 0 || len(input.Manifest) > authoringManifestLimit {
		return provider.Manifest{}, nil, nil, errors.New("plugin test manifest is missing or exceeds 1 MiB")
	}
	manifest, err := provider.Parse(input.Manifest)
	if err != nil {
		return provider.Manifest{}, nil, nil, err
	}
	if err := validateFixtureExpectations(input.Cases); err != nil {
		return provider.Manifest{}, nil, nil, err
	}
	return manifest, nil, input.Cases, nil
}

func decodeFixtures(data []byte) ([]authoringFixture, error) {
	if len(data) > authoringInputLimit {
		return nil, errors.New("plugin test fixtures exceed 16 MiB")
	}
	var fixtures []authoringFixture
	if err := decodeStrict(data, &fixtures); err != nil {
		return nil, fmt.Errorf("invalid plugin test fixtures: %w", err)
	}
	if err := validateFixtureExpectations(fixtures); err != nil {
		return nil, err
	}
	return fixtures, nil
}

func validateFixtureExpectations(fixtures []authoringFixture) error {
	for i, fixture := range fixtures {
		if fixture.Expect == nil {
			continue
		}
		for j, diagnostic := range fixture.Expect.Diagnostics {
			if diagnostic.TemplateEffect != "" && !diagnostic.TemplateEffect.Valid() {
				return fmt.Errorf("case %d expect.diagnostics[%d].template_effect has unknown value %q", i, j, diagnostic.TemplateEffect)
			}
		}
	}
	return nil
}

func runAuthoringCase(parent context.Context, tester *process.AuthoringTester, fixture authoringFixture, seen map[string]bool) authoringCaseResult {
	result := authoringCaseResult{Name: fixture.Name, Variables: map[string]string{}}
	var problems []string
	if fixture.Name == "" || seen[fixture.Name] {
		problems = append(problems, "case name must be nonempty and unique")
	}
	seen[fixture.Name] = true
	if fixture.Expect == nil && len(fixture.ExpectConfig) == 0 && fixture.ExpectVars == nil && fixture.ExpectError == "" {
		problems = append(problems, "case needs at least one expectation")
	}
	ctx, cancel := context.WithTimeout(parent, process.AuthoringTimeout)
	defer cancel()
	actual, err := tester.Evaluate(ctx, fixture.Request, fixture.Stdout, fixture.Variables, fixture.ExitCode)
	if actual.Config != nil {
		result.Config = actual.Config
	}
	if actual.Variables != nil {
		result.Variables = actual.Variables
	}
	result.Result = actual.Result
	if err != nil {
		result.Error = err.Error()
	}
	if fixture.ExpectError != "" {
		if err == nil {
			problems = append(problems, fmt.Sprintf("expected pipeline error containing %q, got no pipeline error", fixture.ExpectError))
		} else if !strings.Contains(err.Error(), fixture.ExpectError) {
			problems = append(problems, fmt.Sprintf("expected pipeline error containing %q, got: %s", fixture.ExpectError, err.Error()))
		}
	} else if err != nil {
		problems = append(problems, "pipeline error: "+err.Error())
	}
	if err == nil {
		if len(fixture.ExpectConfig) != 0 && !jsonEqual(fixture.ExpectConfig, actual.Config) {
			problems = append(problems, "effective config did not match expect_config")
		}
		if fixture.ExpectVars != nil && !maps.Equal(fixture.ExpectVars, actual.Variables) {
			problems = append(problems, "effective variables did not match expect_vars")
		}
		if fixture.Expect != nil {
			if actual.Result == nil || !jsonEqual(mustJSON(fixture.Expect), mustJSON(actual.Result)) {
				problems = append(problems, "decoded result did not match expect")
			}
		}
	} else if fixture.ExpectError != "" && (fixture.Expect != nil || len(fixture.ExpectConfig) != 0 || fixture.ExpectVars != nil) {
		problems = append(problems, "cannot check result/config/variables after a pipeline error")
	}
	result.Passed = len(problems) == 0
	if !result.Passed {
		result.Error = strings.Join(problems, "; ")
	}
	return result
}

func jsonEqual(a, b []byte) bool {
	var left, right any
	leftDecoder, rightDecoder := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	leftDecoder.UseNumber()
	rightDecoder.UseNumber()
	return leftDecoder.Decode(&left) == nil && rightDecoder.Decode(&right) == nil && reflect.DeepEqual(left, right)
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

func readBounded(reader io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	return data, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
