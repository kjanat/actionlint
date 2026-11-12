package scripts

import (
	"cmp"
	"slices"

	runnerjson "actionlint.kjanat.dev/v2/internal/languages/json"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

// NameComparison describes name lookup, independently of filesystem behavior.
type NameComparison string

const (
	NameSensitive   NameComparison = "sensitive"
	NameInsensitive NameComparison = "insensitive"
	NameUnknown     NameComparison = "unknown"
)

// EnvironmentScope identifies the declaration that first introduced a name.
type EnvironmentScope string

const (
	EnvironmentWorkflow EnvironmentScope = "workflow"
	EnvironmentJob      EnvironmentScope = "job"
	EnvironmentCaller   EnvironmentScope = "caller"
	EnvironmentStep     EnvironmentScope = "step"
	EnvironmentRuntime  EnvironmentScope = "runtime"
)

// EnvironmentLayer lists known exported names in insertion order.
// UnknownNames is true when the layer can introduce additional names.
// Layers are supplied from the outermost scope to the innermost scope.
type EnvironmentLayer struct {
	Scope        EnvironmentScope
	Names        []string
	UnknownNames bool
}

// DeclaredEnvironment reads keys without applying the generic mapping's
// case-insensitive lookup. Source positions restore YAML declaration order;
// expression-generated keys with the same origin preserve their supplied order.
func DeclaredEnvironment(scope EnvironmentScope, declarations mapping.Mapping) EnvironmentLayer {
	layer := EnvironmentLayer{Scope: scope, UnknownNames: !declarations.Complete()}
	entries := slices.Collect(declarations.Declarations())
	slices.SortStableFunc(entries, func(a, b mapping.Declaration) int {
		left, right := a.Origin.Span.Start, b.Origin.Span.Start
		return cmp.Or(cmp.Compare(left.Line, right.Line), cmp.Compare(left.Column, right.Column))
	})
	for _, entry := range entries {
		if entry.State == mapping.KnownKey {
			layer.Names = append(layer.Names, entry.Name)
		}
	}
	return layer
}

// EnvironmentVariableName describes the exported key, not its secret or value.
// CaseKnown is false when an earlier unresolved Windows declaration could have
// introduced a different spelling of the same case-insensitive key.
type EnvironmentVariableName struct {
	Name      string
	Scope     EnvironmentScope
	CaseKnown bool
}

// EnvironmentNames records merged names and lookup rules for one operating system.
type EnvironmentNames struct {
	operatingSystem runnerapi.OS
	merge, lookup   NameComparison
	names           []EnvironmentVariableName
	unknownNames    bool
}

func (e EnvironmentNames) OperatingSystem() runnerapi.OS    { return e.operatingSystem }
func (e EnvironmentNames) MergeComparison() NameComparison  { return e.merge }
func (e EnvironmentNames) LookupComparison() NameComparison { return e.lookup }
func (e EnvironmentNames) UnknownNames() bool               { return e.unknownNames }
func (e EnvironmentNames) Names() []EnvironmentVariableName { return slices.Clone(e.names) }

// EnvironmentNamePresence describes lookup in the supplied layers.
// Unknown layers prevent a missing name from being classified as undeclared.
type EnvironmentNamePresence string

const (
	EnvironmentDeclared   EnvironmentNamePresence = "declared"
	EnvironmentUndeclared EnvironmentNamePresence = "undeclared"
	EnvironmentUncertain  EnvironmentNamePresence = "uncertain"
)

func environmentNameKey(name string, comparison NameComparison) string {
	if comparison == NameInsensitive {
		return runnerjson.OrdinalIgnoreCaseKey(name)
	}
	return name
}

// Lookup checks the script's name access against merged declarations.
func (e EnvironmentNames) Lookup(name string) EnvironmentNamePresence {
	uncertain := e.unknownNames
	for _, variable := range e.names {
		if variable.CaseKnown && variable.Name == name {
			return EnvironmentDeclared
		}
		foldedMatch := environmentNameKey(variable.Name, NameInsensitive) == environmentNameKey(name, NameInsensitive)
		switch e.lookup {
		case NameInsensitive:
			if foldedMatch {
				return EnvironmentDeclared
			}
		case NameSensitive:
			uncertain = uncertain || !variable.CaseKnown && foldedMatch
		case NameUnknown:
			uncertain = uncertain || foldedMatch
		}
	}
	if uncertain {
		return EnvironmentUncertain
	}
	return EnvironmentUndeclared
}

// EnvironmentAlternatives preserves OS-dependent outcomes instead of selecting
// the developer machine's behavior. An unknown OS expands to Linux, macOS and
// Windows; duplicate alternatives are removed without changing their order.
func EnvironmentAlternatives(shell string, operatingSystems []runnerapi.OS, layers ...EnvironmentLayer) []EnvironmentNames {
	var systems []runnerapi.OS
	for _, system := range operatingSystems {
		if system != runnerapi.Linux && system != runnerapi.MacOS && system != runnerapi.Windows {
			systems = append(systems, runnerapi.Linux, runnerapi.MacOS, runnerapi.Windows)
		} else {
			systems = append(systems, system)
		}
	}
	if len(systems) == 0 {
		systems = []runnerapi.OS{runnerapi.Linux, runnerapi.MacOS, runnerapi.Windows}
	}
	var alternatives []EnvironmentNames
	seen := make(map[runnerapi.OS]bool)
	for _, system := range systems {
		if seen[system] {
			continue
		}
		seen[system] = true
		e := EnvironmentNames{operatingSystem: system, merge: NameSensitive, lookup: NameUnknown}
		if system == runnerapi.Windows {
			e.merge = NameInsensitive
		}
		switch ShellExecutableName(shell) {
		case "bash", "sh", "dash", "zsh", "ksh", "fish":
			e.lookup = NameSensitive
		case "pwsh", "powershell":
			e.lookup = e.merge
		case "cmd":
			if system == runnerapi.Windows {
				e.lookup = NameInsensitive
			}
		}
		byName := make(map[string]int)
		for _, layer := range layers {
			e.unknownNames = e.unknownNames || layer.UnknownNames
			for _, name := range layer.Names {
				key := environmentNameKey(name, e.merge)
				if _, exists := byName[key]; exists {
					continue // The runner preserves the spelling first inserted.
				}
				byName[key] = len(e.names)
				e.names = append(e.names, EnvironmentVariableName{Name: name, Scope: layer.Scope, CaseKnown: e.merge == NameSensitive || !e.unknownNames})
			}
		}
		alternatives = append(alternatives, e)
	}
	return alternatives
}
