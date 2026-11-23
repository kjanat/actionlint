package scripts

import (
	"cmp"
	"encoding/json"
	"slices"

	"actionlint.kjanat.dev/v2/internal/languages/runnerjson"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

type NameComparison string

const (
	NameSensitive   NameComparison = "sensitive"
	NameInsensitive NameComparison = "insensitive"
	NameUnknown     NameComparison = "unknown"
)

type EnvironmentScope string

const (
	EnvironmentWorkflow EnvironmentScope = "workflow"
	EnvironmentJob      EnvironmentScope = "job"
	EnvironmentCaller   EnvironmentScope = "caller"
	EnvironmentStep     EnvironmentScope = "step"
	EnvironmentRuntime  EnvironmentScope = "runtime"
)

// EnvironmentLayer lists known exported names in insertion order. Callers
// supply layers from the outermost scope to the innermost scope.
type EnvironmentLayer struct {
	Scope        EnvironmentScope
	Names        []string
	UnknownNames bool
}

// DeclaredEnvironment reads keys without the generic mapping's
// case-insensitive lookup and sorts them in YAML declaration order.
func DeclaredEnvironment(scope EnvironmentScope, declarations mappings.Mapping) EnvironmentLayer {
	layer := EnvironmentLayer{Scope: scope, UnknownNames: !declarations.Complete()}
	entries := slices.Collect(declarations.Declarations())
	slices.SortStableFunc(entries, func(a, b mappings.Declaration) int {
		left, right := a.Origin.Span.Start, b.Origin.Span.Start
		return cmp.Or(cmp.Compare(left.Line, right.Line), cmp.Compare(left.Column, right.Column))
	})
	for _, entry := range entries {
		if entry.State == mappings.KnownKey {
			layer.Names = append(layer.Names, entry.Name)
		}
	}
	return layer
}

// EnvironmentVariableName holds no secret or value. CaseKnown is false when an
// earlier unresolved Windows declaration could have introduced a different
// spelling of the same case-insensitive key.
type EnvironmentVariableName struct {
	Name      string           `json:"name"`
	Scope     EnvironmentScope `json:"scope"`
	CaseKnown bool             `json:"case_known"`
}

// EnvironmentNames is immutable after construction, and its accessors return copies.
type EnvironmentNames struct {
	operatingSystem runnerapi.OS
	merge, lookup   NameComparison
	names           []EnvironmentVariableName `clone:"shared"`
	unknownNames    bool
}

func (e EnvironmentNames) OperatingSystem() runnerapi.OS    { return e.operatingSystem }
func (e EnvironmentNames) MergeComparison() NameComparison  { return e.merge }
func (e EnvironmentNames) LookupComparison() NameComparison { return e.lookup }
func (e EnvironmentNames) UnknownNames() bool               { return e.unknownNames }
func (e EnvironmentNames) Names() []EnvironmentVariableName { return slices.Clone(e.names) }

type environmentNamesJSON struct {
	OperatingSystem  runnerapi.OS              `json:"operating_system"`
	MergeComparison  NameComparison            `json:"merge_comparison"`
	LookupComparison NameComparison            `json:"lookup_comparison"`
	Names            []EnvironmentVariableName `json:"names"`
	UnknownNames     bool                      `json:"unknown_names"`
}

func (e EnvironmentNames) MarshalJSON() ([]byte, error) {
	names := e.names
	if names == nil {
		names = []EnvironmentVariableName{}
	}
	return json.Marshal(environmentNamesJSON{
		OperatingSystem: e.operatingSystem, MergeComparison: e.merge, LookupComparison: e.lookup,
		Names: names, UnknownNames: e.unknownNames,
	})
}

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

// EnvironmentAlternatives returns one outcome per candidate OS and takes no
// behavior from the developer machine. An unknown OS expands to Linux, macOS
// and Windows.
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
