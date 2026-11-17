package component

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine/enginediagnostics"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"github.com/kjanat/wacogo"
)

const (
	maxScriptInputBytes  = 8 << 20
	maxScriptResultBytes = 8 << 20
	maxScriptDiagnostics = 10000
)

var (
	_ scriptengine.Backend         = (*Plugin)(nil)
	_ scriptengine.ConfigValidator = (*Plugin)(nil)
)

// HasScriptEngine reports whether the component implements the snippet role.
func (p *Plugin) HasScriptEngine() bool { return p.scriptDescriptor != nil }

// ScriptDescriptor returns transport-independent snippet engine metadata.
func (p *Plugin) ScriptDescriptor() scriptengine.Descriptor {
	if p.scriptDescriptor == nil {
		return scriptengine.Descriptor{}
	}
	return p.scriptDescriptor.Clone()
}

// loadScriptEngine describes the engine through the script-engine version it
// exports, which extensionRoles negotiated.
func (p *Plugin) loadScriptEngine(ctx context.Context, staticMetadata []byte) error {
	value, err := p.callInterface(ctx, p.script.interfaceName(), "describe")
	if err != nil {
		return err
	}
	descriptor, err := liftScriptDescriptor(value, p.reservedRules)
	if err != nil {
		return err
	}
	if err := p.script.liftCapabilities(value, &descriptor); err != nil {
		return err
	}
	if err := verifyScriptMetadata(staticMetadata, p.script.interfaceName(), &descriptor); err != nil {
		return err
	}
	p.scriptDescriptor = &descriptor
	return nil
}

func (p *Plugin) loadTemplateEffects(ctx context.Context, staticMetadata []byte) error {
	value, err := p.callInterface(ctx, p.script.templateEffects().interfaceName(), "contract")
	if err != nil {
		return err
	}
	contract, ok := value.(wacogo.ValString)
	if !ok || string(contract) != scriptengine.TemplateEffectsContract {
		return errors.New("unsupported template effects contract")
	}
	if err := verifyTemplateMetadata(staticMetadata, string(contract)); err != nil {
		return err
	}
	p.scriptDescriptor.TemplateContract = string(contract)
	return nil
}

func verifyTemplateMetadata(data []byte, contract string) error {
	if len(data) == 0 {
		return nil
	}
	var declared struct {
		TemplateContract string `json:"template_contract"`
	}
	if err := jsonv2.Unmarshal(data, &declared); err != nil {
		return err
	}
	if declared.TemplateContract != contract {
		return errors.New("static template contract disagrees with exported contract")
	}
	return nil
}

// ValidateConfig validates engine-specific JSON through the typed guest API.
func (p *Plugin) ValidateConfig(ctx context.Context, config json.RawMessage) error {
	if !p.HasScriptEngine() {
		return errors.New("component does not implement the script-engine role")
	}
	options, err := scriptOptions(config)
	if err != nil {
		return err
	}
	value, err := p.callInterface(ctx, p.script.interfaceName(), "validate-config", wacogo.ValString(options))
	if err != nil {
		return err
	}
	result, ok := value.(*wacogo.ValResult)
	if !ok {
		return liftError{"configuration result", value}
	}
	if result.IsOk() {
		return nil
	}
	message, ok := result.Err().(wacogo.ValString)
	if !ok {
		return liftError{"configuration error", result.Err()}
	}
	return fmt.Errorf("script engine %s: %s", p.scriptDescriptor.ID, message)
}

func scriptOptions(config json.RawMessage) (string, error) {
	if len(config) == 0 {
		return "{}", nil
	}
	if len(config) > maxScriptInputBytes {
		return "", errors.New("script engine options exceed 8 MiB")
	}
	var object map[string]json.RawMessage
	if err := jsonv2.Unmarshal(config, &object); err != nil || object == nil {
		return "", errors.New("script engine options must be a JSON object")
	}
	return string(config), nil
}

// Check analyzes a sanitized snippet and validates the returned findings.
// A script-engine 0.3.0 engine also receives the excluded codes and, when it
// requested the directory resource and the request carries a directory
// grant, that grant mounted read-only for this call.
func (p *Plugin) Check(ctx context.Context, request scriptengine.Request) (diagnostics []scriptengine.Diagnostic, err error) {
	if !p.HasScriptEngine() {
		return nil, errors.New("component does not implement the script-engine role")
	}
	if request.Filename == "" || strings.ContainsRune(request.Filename, 0) ||
		!utf8.ValidString(request.Source) || !utf8.ValidString(request.Filename) || !validExcluded(request.Excluded) {
		return nil, errors.New("script engine needs UTF-8 source, a nonempty virtual filename and excluded codes without NUL")
	}
	size := uint64(len(request.Source)) + uint64(len(request.Filename)) + uint64(len(request.Config)) + scriptEnvironmentBytes(request.Environment)
	for _, code := range request.Excluded {
		size += uint64(len(code))
	}
	if size > maxScriptInputBytes {
		return nil, errors.New("script engine request exceeds 8 MiB")
	}
	options, err := scriptOptions(request.Config)
	if err != nil {
		return nil, err
	}
	input, grant := p.script.lowerRequest(request, options, p.scriptDescriptor)
	contract := p.script.interfaceName()
	ctx = context.WithValue(ctx, environmentContextKey{}, snippetEnvironmentNames(request.Environment))
	if p.scriptDescriptor.TemplateContract != "" {
		contract = p.script.templateEffects().interfaceName()
	}
	value, err := p.callInterface(context.WithValue(ctx, fileSystemsKey{}, grant.files), contract, "check", input)
	if err != nil {
		return nil, err
	}
	outcome, err := liftRecord(value, "script outcome")
	if err != nil {
		return nil, err
	}
	failure, err := liftScriptOptionalString(outcome.Field("error"), "script error")
	if err != nil {
		return nil, err
	}
	list, err := liftList(outcome.Field("diagnostics"), "script diagnostics")
	if err != nil {
		return nil, err
	}
	if list.Len() > maxScriptDiagnostics {
		return nil, errors.New("script engine returned more than 10000 diagnostics")
	}
	resultBytes := uint64(0)
	if failure != nil {
		resultBytes = uint64(len(*failure))
	}
	if resultBytes > maxScriptResultBytes {
		return nil, errors.New("script engine result exceeds 8 MiB")
	}
	snippet := enginediagnostics.NewSnippet(request.Source, enginediagnostics.UniversalNewlineCoordinates)
	diagnostics = make([]scriptengine.Diagnostic, 0, list.Len())
	for i := range list.Len() {
		value := list.Get(i)
		var effect scriptengine.TemplateEffect
		if p.scriptDescriptor.TemplateContract != "" {
			wrapped, err := liftRecord(value, "template finding")
			if err != nil {
				return nil, err
			}
			value = wrapped.Field("diagnostic")
			tag, err := liftRunnerEnum(wrapped.Field("effect"), p.script.templateEffects().effects)
			if err != nil {
				return nil, err
			}
			effect = scriptengine.TemplateEffect(tag)
			if !effect.Valid() {
				return nil, errors.New("unknown template effect")
			}
		}
		finding, err := p.script.liftDiagnostic(value)
		if err != nil {
			return nil, err
		}
		finding.TemplateEffect = effect
		resultBytes += scriptDiagnosticBytes(finding)
		if resultBytes > maxScriptResultBytes {
			return nil, errors.New("script engine result exceeds 8 MiB")
		}
		if err := p.script.checkDiagnostic(&finding, p.scriptDescriptor, grant); err != nil {
			return nil, err
		}
		if finding.Source == "" && (finding.Filename != request.Filename || finding.Code == "" || finding.Message == "") {
			return nil, errors.New("script engine returned an invalid filename or diagnostic")
		}
		// Guests count universal newlines; the workflow source mapper uses LF
		// rows. A reported source keeps the positions of its own file.
		if err := snippet.Normalize(&finding); err != nil {
			return nil, fmt.Errorf("script engine returned an invalid source range: %w", err)
		}
		diagnostics = append(diagnostics, finding)
	}
	if failure != nil {
		return diagnostics, fmt.Errorf("script engine %s: %s", p.scriptDescriptor.ID, *failure)
	}
	return diagnostics, nil
}

func liftScriptPosition(value wacogo.Val) (sourcetext.Position, error) {
	r, err := liftRecord(value, "script position")
	if err != nil {
		return sourcetext.Position{}, err
	}
	row, rowOK := r.Field("row").(wacogo.ValU32)
	column, columnOK := r.Field("column").(wacogo.ValU32)
	if !rowOK || !columnOK || uint64(row) > math.MaxInt || uint64(column) > math.MaxInt {
		return sourcetext.Position{}, errors.New("invalid script position")
	}
	return sourcetext.Position{Line: int(row), Column: int(column)}, nil
}

func liftScriptOptionalString(value wacogo.Val, name string) (*string, error) {
	option, ok := value.(*wacogo.ValOption)
	if !ok {
		return nil, liftError{name, value}
	}
	if option.IsNone() {
		return nil, nil
	}
	text, ok := option.Val().(wacogo.ValString)
	if !ok {
		return nil, liftError{name, option.Val()}
	}
	result := string(text)
	return &result, nil
}

// liftScriptDiagnostic lifts a script-engine 0.2.0 diagnostic, whose
// severity is an enum.
func liftScriptDiagnostic(value wacogo.Val) (scriptengine.Diagnostic, error) {
	r, err := liftRecord(value, "script diagnostic")
	if err != nil {
		return scriptengine.Diagnostic{}, err
	}
	d, err := liftScriptDiagnosticFields(r)
	if err != nil {
		return d, err
	}
	d.Severity, err = liftRunnerEnum(r.Field("severity"), script_engine_severity)
	return d, err
}

// liftScriptDiagnosticFields lifts the diagnostic fields every
// script-engine version shares.
func liftScriptDiagnosticFields(r *wacogo.ValRecord) (scriptengine.Diagnostic, error) {
	var d scriptengine.Diagnostic
	var err error
	for name, dest := range map[string]*string{"filename": &d.Filename, "code": &d.Code, "name": &d.Name, "message": &d.Message} {
		if *dest, err = liftString(r, name); err != nil {
			return d, err
		}
	}
	if d.Location, err = liftScriptPosition(r.Field("location")); err != nil {
		return d, err
	}
	if d.EndLocation, err = liftScriptPosition(r.Field("end-location")); err != nil {
		return d, err
	}
	url, err := liftScriptOptionalString(r.Field("url"), "diagnostic URL")
	if url != nil {
		d.URL = *url
	}
	return d, err
}

func liftScriptDescriptor(value wacogo.Val, reserved rule.Names) (scriptengine.Descriptor, error) {
	r, err := liftRecord(value, "script descriptor")
	if err != nil {
		return scriptengine.Descriptor{}, err
	}
	var d scriptengine.Descriptor
	for name, dest := range map[string]*string{"id": &d.ID, "version": &d.Version, "name": &d.Name, "engine": &d.Engine, "engine-version": &d.EngineVersion} {
		if *dest, err = liftString(r, name); err != nil {
			return d, err
		}
		if strings.TrimSpace(*dest) == "" {
			return d, fmt.Errorf("script descriptor needs %s", name)
		}
	}
	list, err := liftList(r.Field("shells"), "script shells")
	if err != nil {
		return d, err
	}
	seenShells := make(map[string]struct{}, list.Len())
	for i := range list.Len() {
		shell, ok := list.Get(i).(wacogo.ValString)
		_, duplicate := seenShells[string(shell)]
		if !ok || shell == "" || duplicate {
			return d, errors.New("script descriptor contains an empty or duplicate shell")
		}
		// Shell metadata must use canonical runtime selectors:
		// lowercase executable basenames without the .exe suffix.
		name := string(shell)
		if strings.ContainsAny(name, " /\\\t\r\n") || name != strings.TrimSuffix(strings.ToLower(name), ".exe") {
			return d, fmt.Errorf("script descriptor contains noncanonical shell %q", name)
		}
		seenShells[string(shell)] = struct{}{}
		d.Shells = append(d.Shells, string(shell))
	}
	d.Rule, err = liftScriptRule(r.Field("rule"))
	if err != nil {
		return d, err
	}
	if err := validateSuppressionRuleName(reserved, d.Rule.Name); err != nil {
		return d, err
	}
	if d.Rule.Name != d.Engine || d.Rule.Provider != d.ID {
		return d, errors.New("script rule identity disagrees with its engine descriptor")
	}
	return d, nil
}

func liftScriptRule(value wacogo.Val) (rule.Descriptor, error) {
	r, err := liftRecord(value, "script rule descriptor")
	if err != nil {
		return rule.Descriptor{}, err
	}
	var d rule.Descriptor
	for name, dest := range map[string]*string{"name": &d.Name, "description": &d.Description, "category": &d.Category, "subcategory": &d.Subcategory, "maturity": &d.Maturity, "provider": &d.Provider, "explanation": &d.Explanation, "help": &d.Help, "documentation-url": &d.DocumentationURL} {
		if *dest, err = liftString(r, name); err != nil {
			return d, err
		}
	}
	if d.Recommended, err = liftBool(r, "recommended"); err != nil {
		return d, err
	}
	schema, err := liftString(r, "options-schema")
	if err != nil {
		return d, err
	}
	if d.OptionsSchema, err = validOptionsSchema(schema); err != nil {
		return d, err
	}
	list, err := liftList(r.Field("examples"), "script examples")
	if err != nil {
		return d, err
	}
	for i := range list.Len() {
		r, err := liftRecord(list.Get(i), "script example")
		if err != nil {
			return d, err
		}
		var example rule.Example
		for name, dest := range map[string]*string{"description": &example.Description, "before": &example.Before, "after": &example.After} {
			if *dest, err = liftString(r, name); err != nil {
				return d, err
			}
		}
		d.Examples = append(d.Examples, example)
	}
	return d, nil
}

// verifyScriptMetadata checks the artifact's static metadata against the
// descriptor the engine exported through iface, and takes the action-input
// selectors only the static metadata declares.
func verifyScriptMetadata(data []byte, iface string, d *scriptengine.Descriptor) error {
	if data == nil {
		if len(d.Shells) == 0 {
			return errors.New("script descriptor needs shell or static action-input selectors")
		}
		return nil
	}
	var declared struct {
		MetadataVersion int               `json:"metadata_version"`
		ID              string            `json:"id"`
		Name            string            `json:"name"`
		PluginVersion   string            `json:"plugin_version"`
		Transport       string            `json:"transport"`
		Interface       string            `json:"interface"`
		Rules           []rule.Descriptor `json:"rules"`
		ConfigSchema    json.RawMessage   `json:"config_schema"`
		Engine          struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"engine"`
		Capabilities struct {
			ScriptShells    []string                       `json:"script_shells"`
			ScriptInputs    []checkinputs.ScriptInput      `json:"script_inputs"`
			Fixes           bool                           `json:"fixes"`
			ReportedSources bool                           `json:"reported_sources"`
			Resources       []checkinputs.ToolResourceKind `json:"resources"`
			Format          bool                           `json:"format"`
		} `json:"capabilities"`
	}
	if err := jsonv2.Unmarshal(data, &declared); err != nil {
		return fmt.Errorf("script engine metadata: %w", err)
	}
	capabilities := declared.Capabilities
	if declared.MetadataVersion != 1 || declared.Transport != "wasm-component" ||
		declared.Interface != iface ||
		declared.ID != d.ID || declared.PluginVersion != d.Version || declared.Name != d.Name ||
		declared.Engine.Name != d.Engine || declared.Engine.Version != d.EngineVersion ||
		!slices.Equal(capabilities.ScriptShells, d.Shells) ||
		capabilities.Fixes != d.Fixes || capabilities.ReportedSources != d.ReportedSources ||
		!slices.Equal(capabilities.Resources, d.Resources) || capabilities.Format ||
		len(declared.Rules) != 1 {
		return errors.New("script engine static metadata disagrees with its typed descriptor")
	}
	declaredRule, runtimeRule := declared.Rules[0], d.Rule
	if !sameScriptSchema(declared.ConfigSchema, runtimeRule.OptionsSchema) ||
		!sameScriptSchema(declaredRule.OptionsSchema, runtimeRule.OptionsSchema) {
		return errors.New("script engine static options schema disagrees with its typed descriptor")
	}
	declaredRule.OptionsSchema, runtimeRule.OptionsSchema = nil, nil
	if len(declaredRule.Examples) == 0 {
		declaredRule.Examples = nil
	}
	if !reflect.DeepEqual(declaredRule, runtimeRule) {
		return errors.New("script engine static rule metadata disagrees with its typed descriptor")
	}
	if err := checkinputs.ValidateScriptInputs(declared.Capabilities.ScriptInputs); err != nil {
		return fmt.Errorf("script engine static input selectors: %w", err)
	}
	if len(d.Shells)+len(declared.Capabilities.ScriptInputs) == 0 {
		return errors.New("script descriptor needs shell or static action-input selectors")
	}
	// The host routes scripts using the artifact's static input selectors.
	d.ScriptInputs = slices.Clone(declared.Capabilities.ScriptInputs)
	return nil
}

func sameScriptSchema(left, right []byte) bool {
	return len(left) == 0 && len(right) == 0 || artifacts.SameMetadata(left, right)
}
