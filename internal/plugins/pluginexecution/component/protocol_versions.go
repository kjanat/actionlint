package component

import (
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
	"github.com/kjanat/wacogo"
	wp "github.com/kjanat/wacogo/wasmparser"
)

// versionedRole is one accepted version of a role a component exports
// under a versioned interface name. extensionRoles negotiates it once from
// the component's exports; calls use it and never compare version names.
type versionedRole interface {
	comparable
	// interfaceName is the export name of this version.
	interfaceName() string
}

// interfaceNames lists the export names of versions, in order.
func interfaceNames[V versionedRole](versions []V) []string {
	names := make([]string, len(versions))
	for i, version := range versions {
		names[i] = version.interfaceName()
	}
	return names
}

// runnerProtocol is one runner-provider version. It maps the host's runner
// request and response onto that version's wire records.
type runnerProtocol interface {
	interfaceName() string
	// expectedType is the canonical component type the export must match.
	expectedType() *wp.ComponentType
	lowerRequest(request runnerapi.Request) (wacogo.Val, error)
	// liftHosting reads a candidate's hosting, which only some versions
	// carry.
	liftHosting(candidate *wacogo.ValRecord) (runnerapi.Hosting, error)
	// liftSupport reads and validates the support catalogue of an outcome,
	// which only some versions carry.
	liftSupport(outcome *wacogo.ValRecord, request runnerapi.Request) (runnerapi.SupportCatalog, error)
}

// runnerProtocols are the runner-provider versions this host accepts.
var runnerProtocols = []runnerProtocol{runnerProtocol02{}, runnerProtocol03{}}

// runnerProtocol02 is runner-provider 0.2.0: no hosting, no support catalogue.
type runnerProtocol02 struct{}

func (runnerProtocol02) interfaceName() string           { return RunnerInterface }
func (runnerProtocol02) expectedType() *wp.ComponentType { return expectedRunnerInterface() }
func (runnerProtocol02) lowerRequest(request runnerapi.Request) (wacogo.Val, error) {
	return lowerRunnerRequest(request)
}

func (runnerProtocol02) liftHosting(*wacogo.ValRecord) (runnerapi.Hosting, error) {
	return runnerapi.UnknownHosting, nil
}

func (runnerProtocol02) liftSupport(*wacogo.ValRecord, runnerapi.Request) (runnerapi.SupportCatalog, error) {
	return runnerapi.SupportCatalog{}, nil
}

// runnerProtocol03 is runner-provider 0.3.0: dated service requests,
// candidate hosting and a support catalogue.
type runnerProtocol03 struct{}

func (runnerProtocol03) interfaceName() string           { return Runner03Interface }
func (runnerProtocol03) expectedType() *wp.ComponentType { return expectedRunner03Interface() }
func (runnerProtocol03) lowerRequest(request runnerapi.Request) (wacogo.Val, error) {
	common, err := lowerRunnerRequest(request)
	if err != nil {
		return nil, err
	}
	return lowerRunner03Request(request, common)
}

func (runnerProtocol03) liftHosting(candidate *wacogo.ValRecord) (runnerapi.Hosting, error) {
	return liftRunnerHosting(candidate.Field("hosting"))
}

func (runnerProtocol03) liftSupport(outcome *wacogo.ValRecord, request runnerapi.Request) (runnerapi.SupportCatalog, error) {
	return liftValidatedRunnerSupport(outcome.Field("support"), request.Scope)
}

// templateEffectsProtocol is one template-effects version. It pairs with
// exactly one script-engine version, whose request and diagnostic its
// check uses.
type templateEffectsProtocol struct {
	name          string
	componentType func() *wp.ComponentType
	// effects are the effect enum cases, in wire order.
	effects []string
}

func (t *templateEffectsProtocol) interfaceName() string { return t.name }

// expectedType is the canonical component type the export must match.
func (t *templateEffectsProtocol) expectedType() *wp.ComponentType { return t.componentType() }

var (
	templateEffects02 = &templateEffectsProtocol{TemplateEffectsInterface, expectedTemplateEffectsInterface, template_effects_effect}
	templateEffects03 = &templateEffectsProtocol{TemplateEffects03Interface, expectedTemplateEffects03Interface, template_effects_03_effect}
)

// templateEffectsProtocols are the template-effects versions this host
// accepts.
var templateEffectsProtocols = []*templateEffectsProtocol{templateEffects02, templateEffects03}

// scriptEngineProtocol is one script-engine version. It maps the host's
// script request, descriptor and diagnostics onto that version's wire
// records.
type scriptEngineProtocol interface {
	interfaceName() string
	// expectedType is the canonical component type the export must match.
	expectedType() *wp.ComponentType
	// templateEffects is the template-effects version this one pairs with.
	templateEffects() *templateEffectsProtocol
	// liftCapabilities reads the capabilities of an exported descriptor
	// into d, for versions that declare them.
	liftCapabilities(descriptor wacogo.Val, d *scriptengine.Descriptor) error
	// readStaticCapabilities takes the capabilities of static metadata
	// into d, for versions that declare them.
	readStaticCapabilities(declared staticScriptCapabilities, d *scriptengine.Descriptor) error
	// lowerRequest builds the check request and the filesystem grant the
	// call mounts.
	lowerRequest(request scriptengine.Request, options string, d *scriptengine.Descriptor) (wacogo.Val, scriptGrant)
	liftDiagnostic(value wacogo.Val) (scriptengine.Diagnostic, error)
	// checkDiagnostic validates the version-specific parts of a lifted
	// diagnostic against d and grant.
	checkDiagnostic(finding *scriptengine.Diagnostic, d *scriptengine.Descriptor, grant scriptGrant) error
}

// scriptEngineProtocols are the script-engine versions this host accepts.
var scriptEngineProtocols = []scriptEngineProtocol{scriptEngineProtocol02{}, scriptEngineProtocol03{}}

// scriptEngineProtocolNamed returns the accepted script-engine version
// exported as name.
func scriptEngineProtocolNamed(name string) (scriptEngineProtocol, bool) {
	for _, protocol := range scriptEngineProtocols {
		if protocol.interfaceName() == name {
			return protocol, true
		}
	}
	return nil, false
}

// scriptEngineProtocol02 is script-engine 0.2.0: source, filename and
// options in, enum severities out.
type scriptEngineProtocol02 struct{}

func (scriptEngineProtocol02) interfaceName() string { return ScriptEngineInterface }
func (scriptEngineProtocol02) expectedType() *wp.ComponentType {
	return expectedScriptEngineInterface()
}

func (scriptEngineProtocol02) templateEffects() *templateEffectsProtocol { return templateEffects02 }

func (scriptEngineProtocol02) liftCapabilities(wacogo.Val, *scriptengine.Descriptor) error {
	return nil
}

func (scriptEngineProtocol02) readStaticCapabilities(staticScriptCapabilities, *scriptengine.Descriptor) error {
	return nil
}

func (scriptEngineProtocol02) lowerRequest(request scriptengine.Request, options string, _ *scriptengine.Descriptor) (wacogo.Val, scriptGrant) {
	return wacogo.NewValRecord(field("source", wacogo.ValString(request.Source)),
		field("filename", wacogo.ValString(request.Filename)), field("config", wacogo.ValString(options))), scriptGrant{}
}

func (scriptEngineProtocol02) liftDiagnostic(value wacogo.Val) (scriptengine.Diagnostic, error) {
	return liftScriptDiagnostic(value)
}

func (scriptEngineProtocol02) checkDiagnostic(*scriptengine.Diagnostic, *scriptengine.Descriptor, scriptGrant) error {
	return nil
}

// scriptEngineProtocol03 is script-engine 0.3.0: excluded codes, directory
// grants, capabilities, reported sources and fixes.
type scriptEngineProtocol03 struct{}

func (scriptEngineProtocol03) interfaceName() string { return ScriptEngine03Interface }
func (scriptEngineProtocol03) expectedType() *wp.ComponentType {
	return expectedScriptEngine03Interface()
}

func (scriptEngineProtocol03) templateEffects() *templateEffectsProtocol { return templateEffects03 }

func (scriptEngineProtocol03) liftCapabilities(descriptor wacogo.Val, d *scriptengine.Descriptor) error {
	r, err := liftRecord(descriptor, "script descriptor")
	if err != nil {
		return err
	}
	return liftScriptCapabilities(r.Field("capabilities"), d)
}

func (scriptEngineProtocol03) readStaticCapabilities(declared staticScriptCapabilities, d *scriptengine.Descriptor) error {
	return readStaticScriptCapabilities03(declared, d)
}

func (scriptEngineProtocol03) lowerRequest(request scriptengine.Request, options string, d *scriptengine.Descriptor) (wacogo.Val, scriptGrant) {
	grant := grantScriptResources(request, d)
	return lowerScriptRequest03(request, options, grant), grant
}

func (scriptEngineProtocol03) liftDiagnostic(value wacogo.Val) (scriptengine.Diagnostic, error) {
	return liftScriptDiagnostic03(value)
}

func (scriptEngineProtocol03) checkDiagnostic(finding *scriptengine.Diagnostic, d *scriptengine.Descriptor, grant scriptGrant) error {
	return checkScriptDiagnostic03(finding, d, grant)
}
