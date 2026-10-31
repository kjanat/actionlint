package config

import (
	"errors"
	"fmt"
	"path"
	"slices"

	"actionlint.kjanat.dev/v2/knowledge"
	"actionlint.kjanat.dev/v2/repository"
	"github.com/kjanat/go-yaml/v4"
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

func decodeFact[T any](node *yaml.Node, decode func(*yaml.Node) (T, error)) (*T, error) {
	if node.ShortTag() == "!!null" {
		return new(T), nil
	}
	value, err := decode(node)
	if err != nil {
		return nil, err
	}
	return new(value), nil
}

func repositoryNames(node *yaml.Node) (repository.Names, error) {
	if node.Kind != yaml.SequenceNode {
		return repository.Names{}, fmt.Errorf("repository names must be a sequence at line:%d,col:%d", node.Line, node.Column)
	}
	values := make([]string, len(node.Content))
	for i, item := range node.Content {
		if item.ShortTag() != "!!str" {
			return repository.Names{}, fmt.Errorf("repository names must be strings at line:%d,col:%d", item.Line, item.Column)
		}
		values[i] = item.Value
	}
	return repository.NewNames(values...)
}

func repositoryRunners(node *yaml.Node) (repository.Names, error) {
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

func repositoryVariables(node *yaml.Node) (repository.Variables, error) {
	if node.Kind != yaml.MappingNode {
		return repository.Variables{}, fmt.Errorf("repository variables must be a mapping at line:%d,col:%d", node.Line, node.Column)
	}
	entries := make([]repository.Variable, 0, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		entry := repository.Variable{Name: key.Value}
		if key.ShortTag() != "!!str" {
			return repository.Variables{}, errors.New("variable name must be a string")
		}
		switch value.ShortTag() {
		case "!!str":
			entry.Value = knowledge.Available(value.Value)
		case "!!null":
		default:
			return repository.Variables{}, fmt.Errorf("repository variable %q must have a string or null value", key.Value)
		}
		entries = append(entries, entry)
	}
	return repository.NewVariables(entries...)
}

func repositoryEnvironments(node *yaml.Node) (repository.Environments, error) {
	if node.Kind != yaml.MappingNode {
		return repository.Environments{}, errors.New("repository environments must be a mapping")
	}
	entries := make([]repository.Environment, 0, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		name, value := node.Content[i], node.Content[i+1]
		if name.ShortTag() != "!!str" || value.Kind != yaml.MappingNode {
			return repository.Environments{}, errors.New("repository environment must be a named mapping")
		}
		entry := repository.Environment{Name: name.Value}
		for j := 0; j < len(value.Content); j += 2 {
			key, item := value.Content[j], value.Content[j+1]
			switch key.Value {
			case "variables":
				fact, err := decodeFact(item, repositoryVariables)
				if err != nil {
					return repository.Environments{}, err
				}
				entry.Variables = *fact
			case "secrets":
				fact, err := decodeFact(item, repositoryNames)
				if err != nil {
					return repository.Environments{}, err
				}
				entry.Secrets = *fact
			default:
				return repository.Environments{}, fmt.Errorf("unknown repository environment key %q", key.Value)
			}
		}
		entries = append(entries, entry)
	}
	return repository.NewEnvironments(entries...)
}

func (r *RepositoryConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.ShortTag() == "!!null" {
		*r = RepositoryConfig{reset: true}
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return errors.New("repository must be a mapping")
	}
	next := RepositoryConfig{}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		var err error
		switch key.Value {
		case "default-workflow-permissions":
			next.permissions, err = decodeFact(value, func(n *yaml.Node) (repository.Permissions, error) {
				if n.ShortTag() != "!!str" {
					return repository.Permissions{}, errors.New("default-workflow-permissions must be a string")
				}
				return repository.ParsePermissions(n.Value)
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
			return fmt.Errorf("unknown repository key %q", key.Value)
		}
		if err != nil {
			return err
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

func (r RepositoryConfig) MarshalYAML() (any, error) {
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
			if entry.Variables.Reason() != knowledge.NotFetchedReason {
				encodeFact(environment, "variables", &entry.Variables, encodeVariables)
			}
			if entry.Secrets.Reason() != knowledge.NotFetchedReason {
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
