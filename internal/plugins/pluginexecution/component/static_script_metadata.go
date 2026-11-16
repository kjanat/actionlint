package component

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// ReadScriptMetadata validates the static routing declaration for a script component.
// The loaded component must still verify this declaration against its exports.
func ReadScriptMetadata(data []byte, reserved rule.Names) (checkinputs.ScriptEngineDescriptor, error) {
	var declared struct {
		Interface        string            `json:"interface"`
		ID               string            `json:"id"`
		Name             string            `json:"name"`
		Version          string            `json:"plugin_version"`
		TemplateContract string            `json:"template_contract"`
		Rules            []rule.Descriptor `json:"rules"`
		Engine           struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"engine"`
		Capabilities staticScriptCapabilities `json:"capabilities"`
	}
	if err := jsonv2.Unmarshal(data, &declared); err != nil {
		return checkinputs.ScriptEngineDescriptor{}, fmt.Errorf("script engine metadata: %w", err)
	}
	protocol, ok := scriptEngineProtocolNamed(declared.Interface)
	if !ok {
		return checkinputs.ScriptEngineDescriptor{}, errors.New("metadata does not declare a supported script engine interface")
	}
	if len(declared.Rules) != 1 {
		return checkinputs.ScriptEngineDescriptor{}, errors.New("script engine metadata must declare one rule")
	}
	r := declared.Rules[0]
	d := checkinputs.ScriptEngineDescriptor{ID: declared.ID, Version: declared.Version, Name: declared.Name, Engine: declared.Engine.Name, EngineVersion: declared.Engine.Version, Shells: slices.Clone(declared.Capabilities.Shells), Rule: rule.Descriptor{
		Name: r.Name, Description: r.Description, Category: r.Category, Subcategory: r.Subcategory,
		Maturity: r.Maturity, Provider: r.Provider, Recommended: r.Recommended, Explanation: r.Explanation,
		Help: r.Help, DocumentationURL: r.DocumentationURL, OptionsSchema: slices.Clone(r.OptionsSchema), Examples: slices.Clone(r.Examples),
	}}
	for name, value := range map[string]string{"id": d.ID, "version": d.Version, "name": d.Name, "engine": d.Engine, "engine-version": d.EngineVersion} {
		if strings.TrimSpace(value) == "" {
			return checkinputs.ScriptEngineDescriptor{}, fmt.Errorf("script descriptor needs %s", name)
		}
	}
	if len(d.Shells) == 0 {
		d.Shells = nil
	}
	seenShells := map[string]bool{}
	for _, shell := range d.Shells {
		if shell == "" || seenShells[shell] {
			return checkinputs.ScriptEngineDescriptor{}, errors.New("script descriptor contains an empty or duplicate shell")
		}
		if strings.ContainsAny(shell, " /\\\t\r\n") || shell != strings.TrimSuffix(strings.ToLower(shell), ".exe") {
			return checkinputs.ScriptEngineDescriptor{}, fmt.Errorf("script descriptor contains noncanonical shell %q", shell)
		}
		seenShells[shell] = true
	}
	if err := validateSuppressionRuleName(reserved, d.Rule.Name); err != nil {
		return checkinputs.ScriptEngineDescriptor{}, err
	}
	if d.Rule.Name != d.Engine || d.Rule.Provider != d.ID {
		return checkinputs.ScriptEngineDescriptor{}, errors.New("script rule identity disagrees with its engine descriptor")
	}
	if len(d.Rule.Examples) == 0 {
		d.Rule.Examples = nil
	}
	if len(d.Rule.OptionsSchema) != 0 && !json.Valid(d.Rule.OptionsSchema) {
		return checkinputs.ScriptEngineDescriptor{}, errors.New("plugin options schema is not JSON")
	}
	if err := protocol.readStaticCapabilities(declared.Capabilities, &d); err != nil {
		return checkinputs.ScriptEngineDescriptor{}, err
	}
	if err := verifyScriptMetadata(data, declared.Interface, &d); err != nil {
		return checkinputs.ScriptEngineDescriptor{}, err
	}
	if declared.TemplateContract != "" && declared.TemplateContract != checkinputs.TemplateEffectsContract {
		return checkinputs.ScriptEngineDescriptor{}, errors.New("unsupported template effects contract")
	}
	d.TemplateContract = declared.TemplateContract
	return d, nil
}
