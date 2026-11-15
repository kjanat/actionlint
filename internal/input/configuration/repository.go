package configuration

import (
	"errors"
	"fmt"
	"path"
	"reflect"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"

	"actionlint.kjanat.dev/v2/internal/model/availability"
	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

// RepositoryWire defines the configuration fields shared by the schema and overlay.
type RepositoryWire struct {
	Service      any                `yaml:"service"`
	Permissions  *string            `yaml:"default-workflow-permissions" jsonschema:"nullable,enum=read,enum=write,description=Repository default workflow token permissions. Omission inherits host knowledge; null makes the default unknown."`
	Runners      []string           `yaml:"runners" jsonschema:"nullable,description=Additional runner labels or Go path.Match patterns. Empty permits no additional labels."`
	Variables    map[string]*string `yaml:"variables" jsonschema:"nullable,description=Available variable names with optional string values. Null values retain known names without assuming values. Values are never included in diagnostics."`
	Secrets      []string           `yaml:"secrets" jsonschema:"nullable,description=Available secret names. Values are never accepted. Empty permits only built-in and workflow_call secrets."`
	Environments map[string]struct {
		Variables map[string]*string `yaml:"variables" jsonschema:"nullable,description=Available variable names with optional string values for this environment. Null values retain known names without assuming values. Values are never included in diagnostics."`
		Secrets   []string           `yaml:"secrets" jsonschema:"nullable,description=Available secret names for this environment. Values are never accepted."`
	} `yaml:"environments" jsonschema:"nullable,description=Known deployment environments and their variable and secret names."`
}

// MergeWire exposes the repository's wire shape to configuration plan compilation.
func (RepositoryConfig) MergeWire() reflect.Type { return reflect.TypeFor[RepositoryWire]() }

// RepositoryConfig supplies overrides for host knowledge. Omission inherits the
// host fact; null makes it unknown; an empty collection is a known empty set.
type RepositoryConfig struct {
	reset        bool
	service      *services.Service        `clone:"shared"`
	permissions  *repository.Permissions  `clone:"shared"`
	runners      *repository.Names        `clone:"shared"`
	variables    *repository.Variables    `clone:"shared"`
	secrets      *repository.Names        `clone:"shared"`
	environments *repository.Environments `clone:"shared"`
}

// Apply overlays explicit selections after configuration inheritance is resolved.
func (r RepositoryConfig) Apply(host repository.Snapshot) repository.Snapshot {
	if r.reset {
		host = repository.Snapshot{}
	}
	if r.service != nil {
		host.Service = *r.service
	}
	if r.permissions != nil {
		host.DefaultWorkflowPermissions = *r.permissions
	}
	if r.runners != nil {
		host.RunnerLabels = *r.runners
	}
	if r.variables != nil {
		host.Variables = *r.variables
	}
	if r.secrets != nil {
		host.Secrets = *r.secrets
	}
	if r.environments != nil {
		host.Environments = *r.environments
	}
	return host
}

func decodeFact[T any](value any, decode func(any) (T, error)) (*T, error) {
	if value == nil {
		return new(T), nil
	}
	selected, err := decode(value)
	if err != nil {
		return nil, err
	}
	return new(selected), nil
}

func repositoryNames(value any) (repository.Names, error) {
	if value == nil {
		return repository.Names{}, errors.New("repository names must be a sequence")
	}
	var values []string
	if err := binding.Decode(value, &values); err != nil {
		return repository.Names{}, err
	}
	return repository.NewNames(values...)
}

func repositoryRunners(node any) (repository.Names, error) {
	names, err := repositoryNames(node)
	if err != nil {
		return repository.Names{}, err
	}
	for _, pattern := range names.Values() {
		if _, err := path.Match(pattern, ""); err != nil {
			return repository.Names{}, fmt.Errorf("invalid repository runner label pattern %q: %w", pattern, err)
		}
	}
	return names, nil
}

func repositoryVariables(value any) (repository.Variables, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return repository.Variables{}, errors.New("repository variables must be a mapping")
	}
	entries := make([]repository.Variable, 0, len(object))
	for key, value := range object {
		entry := repository.Variable{Name: key}
		switch selected := value.(type) {
		case string:
			entry.Value = availability.Available(selected)
		case nil:
		default:
			return repository.Variables{}, binding.At(key, fmt.Errorf("repository variable %q must have a string or null value", key))
		}
		entries = append(entries, entry)
	}
	return repository.NewVariables(entries...)
}

func repositoryEnvironments(value any) (repository.Environments, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return repository.Environments{}, errors.New("repository environments must be a mapping")
	}
	entries := make([]repository.Environment, 0, len(object))
	for name, value := range object {
		fields, ok := value.(map[string]any)
		if !ok {
			return repository.Environments{}, binding.At(name, errors.New("repository environment must be a named mapping"))
		}
		entry := repository.Environment{Name: name}
		for key, item := range fields {
			switch key {
			case "variables":
				fact, err := decodeFact(item, repositoryVariables)
				if err != nil {
					return repository.Environments{}, binding.At(name, binding.At(key, err))
				}
				entry.Variables = *fact
			case "secrets":
				fact, err := decodeFact(item, repositoryNames)
				if err != nil {
					return repository.Environments{}, binding.At(name, binding.At(key, err))
				}
				entry.Secrets = *fact
			default:
				return repository.Environments{}, binding.At(name, binding.At(key, fmt.Errorf("unknown repository environment key %q", key)))
			}
		}
		entries = append(entries, entry)
	}
	return repository.NewEnvironments(entries...)
}

func (r *RepositoryConfig) DecodeConfig(value any) error {
	if value == nil {
		*r = RepositoryConfig{reset: true}
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return errors.New("repository must be a mapping")
	}
	var next RepositoryConfig
	for key, value := range object {
		var err error
		switch key {
		case "service":
			next.service, err = decodeFact(value, repositoryService)
		case "default-workflow-permissions":
			next.permissions, err = decodeFact(value, func(value any) (repository.Permissions, error) {
				selected, ok := value.(string)
				if !ok {
					return repository.Permissions{}, errors.New("default-workflow-permissions must be a string")
				}
				return repository.ParsePermissions(selected)
			})
		case "runners":
			next.runners, err = decodeFact(value, repositoryRunners)
		case "variables":
			next.variables, err = decodeFact(value, repositoryVariables)
		case "secrets":
			next.secrets, err = decodeFact(value, repositoryNames)
		case "environments":
			next.environments, err = decodeFact(value, repositoryEnvironments)
		default:
			return binding.At(key, fmt.Errorf("unknown repository key %q", key))
		}
		if err != nil {
			return binding.At(key, err)
		}
	}
	*r = next
	return nil
}

func encodeFact[T interface{ Available() bool }, V any](target map[string]any, key string, selected *T, encode func(T) V) {
	if selected == nil {
		return
	}
	target[key] = nil
	if value := *selected; value.Available() {
		target[key] = encode(value)
	}
}

func encodeVariables(variables repository.Variables) map[string]*string {
	values := make(map[string]*string)
	for _, entry := range variables.Entries() {
		value, ok := entry.Value.Value()
		if ok {
			values[entry.Name] = new(value)
		} else {
			values[entry.Name] = nil
		}
	}
	return values
}

func (r RepositoryConfig) EncodeConfig() (any, error) {
	if r.reset {
		return nil, nil
	}
	value := map[string]any{}
	if r.service != nil {
		switch {
		case r.service.Kind() == services.Unknown:
			value["service"] = nil
		case r.service.Version() == "":
			value["service"] = string(r.service.Kind())
		default:
			value["service"] = map[string]string{"kind": string(r.service.Kind()), "version": r.service.Version()}
		}
	}
	encodeFact(value, "default-workflow-permissions", r.permissions, repository.Permissions.String)
	encodeFact(value, "runners", r.runners, repository.Names.Values)
	encodeFact(value, "variables", r.variables, encodeVariables)
	encodeFact(value, "secrets", r.secrets, repository.Names.Values)
	encodeFact(value, "environments", r.environments, func(environments repository.Environments) map[string]map[string]any {
		values := make(map[string]map[string]any)
		for _, entry := range environments.Entries() {
			environment := map[string]any{}
			if entry.Variables.Reason() != availability.NotFetchedReason {
				encodeFact(environment, "variables", &entry.Variables, encodeVariables)
			}
			if entry.Secrets.Reason() != availability.NotFetchedReason {
				encodeFact(environment, "secrets", &entry.Secrets, repository.Names.Values)
			}
			values[entry.Name] = environment
		}
		return values
	})
	return value, nil
}

func repositoryService(value any) (services.Service, error) {
	if kind, ok := value.(string); ok {
		if kind == "" {
			return services.Service{}, errors.New("service must not be empty; use null or \"unknown\"")
		}
		return services.Parse(kind, "")
	}
	var selected struct {
		Kind    string `yaml:"kind"`
		Version string `yaml:"version"`
	}
	if err := binding.Decode(value, &selected); err != nil {
		return services.Service{}, err
	}
	if selected.Kind != string(services.GitHubEnterpriseServer) {
		return services.Service{}, errors.New("service mapping requires kind: ghes; use a string for other services")
	}
	return services.Parse(selected.Kind, selected.Version)
}

// RepositoryFacts returns facts independently of the diagnostic preset.
func RepositoryFacts(cfg *Config, host repository.Snapshot) repository.Snapshot {
	if cfg == nil {
		return host
	}
	return cfg.Repository.Apply(host)
}

// Facts returns only the repository overrides declared by this document.
func (cfg *Config) Facts() repository.Snapshot { return RepositoryFacts(cfg, repository.Snapshot{}) }

// VariableNames and SecretNames preserve nil for unavailable name sets.
func VariableNames(facts repository.Snapshot) []string {
	if variables, ok := facts.Variables.Value(); ok {
		return variables.Names()
	}
	return nil
}

func SecretNames(facts repository.Snapshot) []string {
	if secrets, ok := facts.Secrets.Value(); ok {
		return secrets.Values()
	}
	return nil
}

func RunnerLabels(facts repository.Snapshot) []string {
	if labels, ok := facts.RunnerLabels.Value(); ok {
		return slices.Clone(labels.Values())
	}
	return nil
}

// CopyConfig copies the owned configuration state.
func (r RepositoryConfig) CopyConfig() (any, error) { return copyOwnedRepositoryConfig(r), nil }
