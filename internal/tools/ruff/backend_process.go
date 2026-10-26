package ruff

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/scriptcoords"
)

// Execute is the process adapter's only execution capability. It returns after
// the child exits. The host supplies cancellation, environment and process limits.
type Execute func(context.Context, []string, string) ([]byte, error)

// ProcessBackend implements the same semantic check contract as an embedded
// engine. Arguments and stdout are private to this adapter.
type ProcessBackend struct {
	protocol      *commandPolicy
	execute       Execute
	compatibility *Compatibility
	warning       func(error)
	policy        versionProbe
}

// NewProcessBackend returns a native adapter and the launcher argument prefix.
// Extra flags follow the legacy command-line contract; engine config travels in
// ScriptRequest instead. Callers must finish configuration before concurrent use.
func NewProcessBackend(execute Execute, executable string, args []string, directory string) (*ProcessBackend, []string) {
	protocol := &commandPolicy{workingDirectory: directory}
	prefix := protocol.CommandArguments(executable, args)
	return &ProcessBackend{protocol: protocol, execute: execute}, prefix
}

func (p *ProcessBackend) OptionalVersion(check *Compatibility, warning func(error)) {
	p.compatibility, p.warning = check, warning
}

// TemplateContract declares the adapter's template-effects vocabulary.
func (p *ProcessBackend) TemplateContract() string { return analyzer.TemplateEffectsContract }

func (p *ProcessBackend) checkPolicy(ctx context.Context) error {
	err := p.policy.run(ctx, func() (string, error) {
		var version string
		if p.compatibility != nil {
			version = p.compatibility.probe.version()
		} else {
			output, err := p.execute(ctx, []string{"--version"}, "")
			if err != nil {
				return "", err
			}
			version = string(output)
		}
		return version, checkTemplateVersion(version)
	}, p.warning)
	return err
}

func (p *ProcessBackend) available(ctx context.Context) (bool, error) {
	if p.compatibility == nil {
		return true, ctx.Err()
	}
	run := func(args []string, source string, callback func([]byte, error) error) {
		output, err := p.execute(ctx, args, source)
		_ = callback(output, err)
	}
	return p.compatibility.available(ctx, run, func() error { return nil }, p.warning)
}

// ValidateTemplates restricts provenance filtering to the reviewed engine version.
func (p *ProcessBackend) ValidateTemplates(ctx context.Context) (bool, error) {
	if available, err := p.available(ctx); err != nil || !available {
		return false, err
	}
	if err := p.checkPolicy(ctx); err != nil {
		if p.compatibility != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (p *ProcessBackend) Check(ctx context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.Filename != stdinFilename {
		return nil, fmt.Errorf("unsupported Ruff virtual filename %q", request.Filename)
	}
	if err := p.protocol.validateCommand(request.Filename); err != nil {
		return nil, err
	}
	config := Config{}
	if len(request.Config) != 0 {
		decoder := json.NewDecoder(bytes.NewReader(request.Config))
		decoder.UseNumber()
		if err := decoder.Decode(&config.Native); err != nil {
			return nil, fmt.Errorf("invalid Ruff engine config: %w", err)
		}
	}
	// Ruff combines repeated selector flags in order.
	if target, ok := config.Native["target-version"].(string); ok {
		config.TargetVersion = target
		delete(config.Native, "target-version")
	}
	if lint, ok := config.Native["lint"].(map[string]any); ok {
		for key, target := range map[string]*[]string{"select": &config.Select, "ignore": &config.Ignore} {
			if value, exists := lint[key]; exists && value != nil {
				encoded, err := json.Marshal(value)
				if err != nil {
					return nil, err
				}
				if err := json.Unmarshal(encoded, target); err != nil {
					return nil, err
				}
				delete(lint, key)
			}
		}
		if len(lint) == 0 {
			delete(config.Native, "lint")
		}
	}
	native, err := nativeArguments(config)
	if err != nil {
		return nil, err
	}
	args := append([]string{"check"}, native...)
	args = append(args, p.protocol.flags...)
	args = append(args, arguments(config)[1:]...)
	directory := p.protocol.workingDirectory
	if directory == "" {
		directory = "."
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		return nil, err
	}
	if !directoryInfo.IsDir() {
		return nil, fmt.Errorf("ruff working directory is not a directory: %q", directory)
	}
	if available, err := p.available(ctx); err != nil || !available {
		return nil, err
	}
	output, err := p.execute(ctx, args, request.Source)
	if err != nil {
		return nil, err
	}
	findings, err := Decode(output)
	if err != nil {
		return nil, err
	}
	starts := scriptcoords.Starts(request.Source)
	for i := range findings {
		finding := &findings[i]
		parent, err := os.Stat(filepath.Dir(finding.Filename))
		if !filepath.IsAbs(finding.Filename) || filepath.Base(finding.Filename) != stdinFilename || err != nil || !os.SameFile(directoryInfo, parent) {
			return nil, fmt.Errorf("ruff output refers to unexpected file %q", finding.Filename)
		}
		finding.Filename = request.Filename
		if finding.Location.Row <= len(starts) {
			finding.Location = scriptcoords.SourcePosition(starts, finding.Location)
		}
		if finding.EndLocation.Row > 0 && finding.EndLocation.Row <= len(starts) {
			finding.EndLocation = scriptcoords.SourcePosition(starts, finding.EndLocation)
		}
	}
	return findings, nil
}
