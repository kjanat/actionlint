package ruff

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
)

func TestTemplateEffectsIgnorePresentation(t *testing.T) {
	start := Position{Row: 1, Column: 3}
	masks := templateMasks{quoted: map[Position]string{start: "_0"}}
	finding := Diagnostic{
		Code: "renamed-rule", Message: "entirely different wording",
		Location: Position{Row: 1, Column: 1}, EndLocation: Position{Row: 1, Column: 8},
		TemplateEffect: analyzer.TemplateEffectExportedName,
	}
	if masks.accept(finding) {
		t.Fatal("semantic export finding on mask was retained")
	}
	finding.TemplateEffect = analyzer.TemplateEffectNone
	finding.Code = "F822"
	finding.Message = "Undefined name `_0`"
	if !masks.accept(finding) {
		t.Fatal("presentation text overrode explicit independent effect")
	}
	finding.TemplateEffect = analyzer.TemplateEffectExportedName
	finding.Location = Position{Row: 2, Column: 1}
	finding.EndLocation = Position{Row: 2, Column: 8}
	if !masks.accept(finding) {
		t.Fatal("static export outside the mask was dropped")
	}
}

func TestNativeTemplateCoordinatesUseDecodedSource(t *testing.T) {
	directory := t.TempDir()
	backend, _ := NewProcessBackend(func(_ context.Context, args []string, _ string) ([]byte, error) {
		if slices.Equal(args, []string{"--version"}) {
			return []byte("ruff 0.17.0"), nil
		}
		return json.Marshal([]Diagnostic{{Filename: filepath.Join(directory, stdinFilename), Code: "F821", Message: "missing",
			Location: Position{Row: 2, Column: 7}, EndLocation: Position{Row: 2, Column: 14}}})
	}, "ruff", nil, directory)
	findings, err := backend.Check(t.Context(), analyzer.ScriptRequest{Source: "print(1)\rprint(missing)\r", Filename: stdinFilename})
	if err != nil || len(findings) != 1 {
		t.Fatalf("native check: %v, %v", findings, err)
	}
	if findings[0].Location != (Position{Row: 1, Column: 16}) || findings[0].EndLocation != (Position{Row: 1, Column: 23}) {
		t.Fatalf("native engine coordinates were not mapped: %+v", findings[0])
	}
}

type contractTestBackend struct {
	testBackend
	contract string
}

func (b contractTestBackend) ScriptDescriptor() analyzer.ScriptEngineDescriptor {
	return analyzer.ScriptEngineDescriptor{ID: "unrelated-package", TemplateContract: b.contract}
}

func TestBackendTemplateContractRequired(t *testing.T) {
	for _, contract := range []string{"", "future-contract", analyzer.TemplateEffectsContract} {
		backend := contractTestBackend{contract: contract, testBackend: func(context.Context, analyzer.ScriptRequest) ([]Diagnostic, error) { return nil, nil }}
		checker := NewBackend(t.Context(), backend, reviewExpressionEnd)
		python := "python"
		err := checker.Check("print(${{ inputs.value }})", &python, "test", Config{}, func(Diagnostic) {})
		if (err == nil) != (contract == analyzer.TemplateEffectsContract) {
			t.Fatalf("contract %q: %v", contract, err)
		}
	}
}

func TestDeclaredTemplateContractRequiresExplicitEffect(t *testing.T) {
	for _, effect := range []analyzer.ScriptTemplateEffect{"", "unknown", analyzer.TemplateEffectNone} {
		backend := contractTestBackend{contract: analyzer.TemplateEffectsContract, testBackend: func(context.Context, analyzer.ScriptRequest) ([]Diagnostic, error) {
			return []Diagnostic{{Filename: stdinFilename, Code: "custom", Message: "finding", Location: Position{Row: 1, Column: 1}, TemplateEffect: effect}}, nil
		}}
		checker := NewBackend(t.Context(), backend, reviewExpressionEnd)
		python := "python"
		err := checker.Check("print(1)", &python, "test", Config{}, func(Diagnostic) {})
		if err == nil {
			err = checker.Wait()
		}
		if (err == nil) != (effect == analyzer.TemplateEffectNone) {
			t.Fatalf("effect %q: %v", effect, err)
		}
	}
}

func TestNativeTemplatePolicyFailsClosed(t *testing.T) {
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit", true: "optional"}[optional], func(t *testing.T) {
			probes, warnings := 0, 0
			backend, _ := NewProcessBackend(func(_ context.Context, args []string, _ string) ([]byte, error) {
				if !slices.Equal(args, []string{"--version"}) {
					t.Fatalf("unreviewed engine was invoked: %v", args)
				}
				probes++
				return []byte("ruff 0.18.0"), nil
			}, "ruff", nil, ".")
			if optional {
				backend.OptionalVersion(&Compatibility{}, func(error) { warnings++ })
			}
			for range 2 {
				findings, err := backend.Check(t.Context(), analyzer.ScriptRequest{Source: "print(1)", Filename: stdinFilename})
				if len(findings) != 0 || (err == nil) != optional {
					t.Fatalf("unexpected policy outcome: %v, %v", findings, err)
				}
			}
			if probes != 1 || warnings != boolCount(optional) {
				t.Fatalf("policy repeated: probes=%d warnings=%d", probes, warnings)
			}
		})
	}
}

func TestNativeVersionProbeRetriesAfterCancellation(t *testing.T) {
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit", true: "optional"}[optional], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			probes, checks, warnings := 0, 0, 0
			backend, _ := NewProcessBackend(func(_ context.Context, args []string, _ string) ([]byte, error) {
				if slices.Equal(args, []string{"--version"}) {
					probes++
					if probes == 1 {
						cancel()
						return nil, context.Canceled
					}
					return []byte("ruff 0.17.0"), nil
				}
				checks++
				return []byte("[]"), nil
			}, "ruff", nil, ".")
			if optional {
				backend.OptionalVersion(&Compatibility{}, func(error) { warnings++ })
			}
			request := analyzer.ScriptRequest{Source: "print(1)", Filename: stdinFilename}
			if _, err := backend.Check(ctx, request); !errors.Is(err, context.Canceled) {
				t.Fatalf("first probe: %v", err)
			}
			for range 2 {
				if _, err := backend.Check(t.Context(), request); err != nil {
					t.Fatalf("retry inherited canceled probe: %v", err)
				}
			}
			if probes != 2 || checks != 2 || warnings != 0 {
				t.Fatalf("probes=%d checks=%d warnings=%d", probes, checks, warnings)
			}
		})
	}
}

func TestPercentMaskIsValidConversion(t *testing.T) {
	source, valid, err := Sanitize(`print("%03${{ inputs.kind }}" % 1)`, reviewExpressionEnd)
	if err != nil || !valid || !strings.Contains(source, "%03s") {
		t.Fatalf("mask manufactured an unsupported conversion: %q valid=%v err=%v", source, valid, err)
	}
}

func TestTemplateAdapterRejectsUnreviewedVersion(t *testing.T) {
	for _, version := range []string{"ruff 0.17.1", "ruff 1.0.0", "ruff 0.17.0-dev", "other 0.17.0"} {
		if err := checkTemplateVersion(version); err == nil {
			t.Fatalf("accepted unreviewed adapter version %q", version)
		}
	}
	if err := checkTemplateVersion("ruff 0.17.0"); err != nil {
		t.Fatal(err)
	}
}
