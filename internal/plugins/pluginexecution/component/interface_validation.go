package component

import (
	"bytes"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"iter"
	"runtime/debug"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"github.com/kjanat/wacogo"
	"github.com/kjanat/wacogo/wasmparser"
)

type Interfaces struct {
	Imports []string `json:"imports"`
	Exports []string `json:"exports"`
}

type Roles struct {
	Analyzer          string `json:"analyzer,omitempty"`
	ToolIntegration   string `json:"tool_integration,omitempty"`
	ReferenceResolver string `json:"reference_resolver,omitempty"`
	Runner            string `json:"runner,omitempty"`
	ScriptEngine      string `json:"script_engine,omitempty"`
	TemplateEffects   string `json:"template_effects,omitempty"`
}

func (r Roles) Exports() []string {
	return slices.DeleteFunc([]string{r.Analyzer, r.ToolIntegration, r.ReferenceResolver, r.Runner, r.ScriptEngine, r.TemplateEffects}, func(name string) bool { return name == "" })
}

func (r Roles) ScriptOnly() bool {
	return r.ScriptEngine != "" && r == Roles{ScriptEngine: r.ScriptEngine, TemplateEffects: r.TemplateEffects}
}

type admission struct {
	roles       extensionRoleSet
	required    importRequirements
	invocations []commands.Definition
	script      *declaredScript
}

var ErrUnprovidedInterface = errors.New("unprovided interface")

type AdmissionError struct {
	Err error
}

func (e *AdmissionError) Error() string { return e.Err.Error() }

func (e *AdmissionError) Unwrap() error { return e.Err }

type Evidence interface {
	admit(ctx context.Context) (admission, error)
}

type ComponentBytes []byte

type ListedInterfaces struct {
	Interfaces Interfaces
	Metadata   []byte
}

func Admit(ctx context.Context, evidence Evidence) (Roles, error) {
	admitted, err := evidence.admit(ctx)
	if err != nil {
		return Roles{}, err
	}
	return admitted.roles.names(), nil
}

func (listed ListedInterfaces) admit(context.Context) (admission, error) {
	admitted, err := decide(listed.Interfaces, listed.Metadata)
	if err != nil {
		return admission{}, &AdmissionError{Err: err}
	}
	return admitted, nil
}

func (data ComponentBytes) admit(ctx context.Context) (admission, error) {
	limits, err := Options{}.limits()
	if err != nil {
		return admission{}, err
	}
	engine := sharedComponentCompilation.newEngine(ctx, limits)
	admitted, _, err := checkComponent(ctx, engine, data)
	return admitted, errors.Join(err, engine.Close(context.WithoutCancel(ctx)))
}

func checkComponent(ctx context.Context, engine *wacogo.Engine, data []byte) (admission, *wacogo.Component, error) {
	kind, metadata, err := artifacts.Sniff(data)
	if err != nil {
		return admission{}, nil, &AdmissionError{Err: err}
	}
	if kind != artifacts.Component {
		return admission{}, nil, &AdmissionError{Err: errors.New("plugin requires a WebAssembly component")}
	}
	interfaces, componentType, err := parseInterfaces(data)
	if err != nil {
		return admission{}, nil, &AdmissionError{Err: err}
	}
	admitted, err := decide(interfaces, metadata)
	if err != nil {
		return admission{}, nil, &AdmissionError{Err: err}
	}
	if err := checkRoleTypes(componentType, admitted.roles); err != nil {
		return admission{}, nil, &AdmissionError{Err: err}
	}
	compiled, err := sharedComponentCompilation.load(ctx, engine, data)
	if err != nil && ctx.Err() == nil {
		err = &AdmissionError{Err: err}
	}
	if err != nil {
		return admission{}, nil, err
	}
	return admitted, compiled, nil
}

func decide(interfaces Interfaces, metadata []byte) (admission, error) {
	roles, required, err := negotiate(interfaces)
	if err != nil {
		return admission{}, err
	}
	if err := checkDeclaredInterface(metadata, roles.names()); err != nil {
		return admission{}, err
	}
	admitted := admission{roles: roles, required: required}
	if roles.analyzer {
		var declared struct {
			Definitions []commands.Definition `json:"invocation_definitions"`
		}
		if len(metadata) != 0 {
			if err := json.Unmarshal(metadata, &declared); err != nil {
				return admission{}, err
			}
		}
		if _, err := commands.New(declared.Definitions); err != nil {
			return admission{}, err
		}
		admitted.invocations = declared.Definitions
	}
	if roles.script != nil {
		contract := ""
		if roles.templateEffects != nil {
			contract = scriptengine.TemplateEffectsContract
		}
		if err := verifyTemplateMetadata(metadata, contract); err != nil {
			return admission{}, err
		}
		if len(metadata) != 0 {
			if admitted.script, err = readDeclaredScript(metadata, roles.script.interfaceName()); err != nil {
				return admission{}, err
			}
		}
	}
	return admitted, nil
}

func (r extensionRoleSet) names() Roles {
	var roles Roles
	if r.analyzer {
		roles.Analyzer = AnalyzerInterface
	}
	if r.toolIntegration {
		roles.ToolIntegration = ToolIntegrationInterface
	}
	if r.referenceResolver {
		roles.ReferenceResolver = ReferenceResolverInterface
	}
	if r.runner != nil {
		roles.Runner = r.runner.interfaceName()
	}
	if r.script != nil {
		roles.ScriptEngine = r.script.interfaceName()
	}
	if r.templateEffects != nil {
		roles.TemplateEffects = r.templateEffects.interfaceName()
	}
	return roles
}

func checkDeclaredInterface(metadata []byte, roles Roles) error {
	if len(metadata) == 0 {
		return nil
	}
	var declared struct {
		Interface string `json:"interface"`
	}
	if err := jsonv2.Unmarshal(metadata, &declared); err != nil {
		return fmt.Errorf("plugin metadata: %w", err)
	}
	if declared.Interface == "" || slices.Contains(roles.Exports(), declared.Interface) {
		return nil
	}
	return fmt.Errorf("plugin metadata declares %s; the component exports %s", declared.Interface, strings.Join(roles.Exports(), ", "))
}

func ReadInterfaces(data []byte) (Interfaces, error) {
	interfaces, _, err := parseInterfaces(data)
	return interfaces, err
}

func parseInterfaces(data []byte) (Interfaces, *wasmparser.ComponentType, error) {
	parser := wasmparser.NewValidatingParser(bytes.NewReader(data), wasmparser.DefaultFeatures())
	var imports []wasmparser.ValidatedComponentImport
	var exports []wasmparser.ValidatedComponentExport
	for {
		payload, err := parser.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err == nil {
			switch section := payload.(type) {
			case *wasmparser.ValidatedComponentImportSectionPayload:
				imports, err = appendItems(imports, section.Items())
			case *wasmparser.ValidatedComponentExportSectionPayload:
				exports, err = appendItems(exports, section.Items())
			}
		}
		if err != nil {
			return Interfaces{}, nil, fmt.Errorf("validate component contract: %w", err)
		}
	}
	interfaces := Interfaces{Imports: []string{}, Exports: []string{}}
	for _, item := range imports {
		interfaces.Imports = append(interfaces.Imports, item.Import.Name.Name)
	}
	for _, item := range exports {
		name := item.Export.Name.Name
		if item.Export.Kind == wasmparser.ExternalKindInstance {
			interfaces.Exports = append(interfaces.Exports, name)
		} else if slices.ContainsFunc(acceptedExports(), func(accepted string) bool { return sameFamily(name, accepted) }) {
			return Interfaces{}, nil, fmt.Errorf("plugin export %s must be an interface instance", name)
		}
	}
	return interfaces, parser.TopLevelComponentType(), nil
}

func appendItems[T any](items []T, section iter.Seq2[T, error]) ([]T, error) {
	for item, err := range section {
		if err != nil {
			return items, err
		}
		items = append(items, item)
	}
	return items, nil
}

func acceptedExports() []string {
	return slices.Concat([]string{AnalyzerInterface, ToolIntegrationInterface, ReferenceResolverInterface}, interfaceNames(runnerProtocols), interfaceNames(scriptEngineProtocols), interfaceNames(templateEffectsProtocols))
}

func acceptedInterfaces() []string {
	var required importRequirements
	exports, imports := acceptedExports(), required.hostImports()
	lines := make([]string, 0, len(exports)+len(imports)+len(grantedImportPrefixes))
	for _, name := range exports {
		lines = append(lines, "export "+name)
	}
	for _, provided := range imports {
		lines = append(lines, "import "+provided.name)
	}
	for _, prefix := range grantedImportPrefixes {
		lines = append(lines, "import "+prefix)
	}
	slices.Sort(lines)
	return lines
}

func AcceptedInterfacesDigest() string {
	return sha256hex.Digest([]byte(strings.Join(acceptedInterfaces(), "\n")))
}

var admissionModules = []string{"github.com/kjanat/wacogo", "github.com/kjanat/wazero"}

func AdmissionIdentity() string {
	info, _ := debug.ReadBuildInfo()
	return admissionIdentity(info)
}

func admissionIdentity(info *debug.BuildInfo) string {
	lines := acceptedInterfaces()
	if info != nil {
		lines = append(lines, "main "+info.Main.Path+" "+info.Main.Version+" "+info.Main.Sum)
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" || setting.Key == "vcs.modified" {
				lines = append(lines, setting.Key+" "+setting.Value)
			}
		}
		for _, dependency := range info.Deps {
			if !slices.Contains(admissionModules, dependency.Path) {
				continue
			}
			module := dependency
			if dependency.Replace != nil {
				module = dependency.Replace
			}
			lines = append(lines, "module "+dependency.Path+" "+module.Path+" "+module.Version+" "+module.Sum)
		}
	}
	return sha256hex.Digest([]byte(strings.Join(lines, "\n")))
}

// validateAnalyzerInterface rejects incompatible analyzer interfaces before guest startup.
func validateAnalyzerInterface(data []byte) error {
	return validatePluginInterfaces(data, extensionRoleSet{analyzer: true})
}

func validatePluginInterfaces(data []byte, roles extensionRoleSet) error {
	_, component, err := parseInterfaces(data)
	if err != nil {
		return err
	}
	return checkRoleTypes(component, roles)
}

func checkRoleTypes(component *wasmparser.ComponentType, roles extensionRoleSet) error {
	instance, err := component.NewInstance(nil)
	if err != nil {
		return fmt.Errorf("validate component contract: %w", err)
	}
	if roles.analyzer {
		if err := expectedAnalyzerInterface().CheckInstantiation(map[string]any{"plugin": instance}); err != nil {
			return fmt.Errorf("incompatible analyzer contract: %w", err)
		}
	}
	if roles.referenceResolver {
		if err := expectedReferenceResolverInterface().CheckInstantiation(map[string]any{"plugin": instance}); err != nil {
			return fmt.Errorf("incompatible reference resolver interface: %w", err)
		}
	}
	if roles.toolIntegration {
		if err := expectedToolIntegrationInterface().CheckInstantiation(map[string]any{"plugin": instance}); err != nil {
			return fmt.Errorf("incompatible tool integration contract: %w", err)
		}
	}
	if roles.templateEffects != nil {
		if err := roles.templateEffects.expectedType().CheckInstantiation(map[string]any{"plugin": instance}); err != nil {
			return fmt.Errorf("incompatible %s contract: %w", roles.templateEffects.interfaceName(), err)
		}
	}
	if roles.runner != nil {
		if err := roles.runner.expectedType().CheckInstantiation(map[string]any{"plugin": instance}); err != nil {
			return fmt.Errorf("incompatible runner contract: %w", err)
		}
	}
	if roles.script != nil {
		if err := roles.script.expectedType().CheckInstantiation(map[string]any{"plugin": instance}); err != nil {
			return fmt.Errorf("incompatible %s contract: %w", roles.script.interfaceName(), err)
		}
	}
	return nil
}
