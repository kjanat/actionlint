package config

import (
	"errors"
	"fmt"
	"path"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"

	"actionlint.kjanat.dev/v2/internal/model/availability"
	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
)

// RepositoryConfig supplies overrides for host knowledge. Omission inherits the
// host fact; null makes it unknown; an empty collection is a known empty set.
type RepositoryConfig struct {
	reset        bool
	permissions  *repository.Permissions
	runners      *repository.Names
	variables    *repository.Variables
	secrets      *repository.Names
	environments *repository.Environments
}

// Apply overlays explicit selections after configuration inheritance is resolved.
func (r RepositoryConfig) Apply(host repository.Snapshot) repository.Snapshot {
	if r.reset {
		host = repository.Snapshot{}
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
