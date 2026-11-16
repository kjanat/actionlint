package expressions

import (
	"strings"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	"actionlint.kjanat.dev/v2/internal/input/configuration"

	"actionlint.kjanat.dev/v2/internal/model/repository"

	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

func environmentReferenceNames(variables, secrets, environmentSecrets []string) ReferenceNames {
	var extra exprtypes.ObjectBuilder
	if environmentSecrets == nil {
		extra.SetMapped(exprtypes.StringType())
	}
	for _, name := range environmentSecrets {
		extra.SetProperty(name, exprtypes.StringType())
	}
	return ReferenceNames{Variables: variables, Secrets: secrets, EnvironmentSecrets: new(extra.Build())}
}

func unionNames(repository, environment []string) []string {
	if repository == nil || environment == nil {
		return nil
	}
	names := append([]string{}, repository...)
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		seen[strings.ToLower(name)] = true
	}
	for _, name := range environment {
		key := strings.ToLower(name)
		if !seen[key] {
			names = append(names, name)
			seen[key] = true
		}
	}
	return names
}

func jobReferenceNames(facts repository.Snapshot, job workflows.Job) ReferenceNames {
	names := ReferenceNames{Variables: configuration.VariableNames(facts), Secrets: configuration.SecretNames(facts)}
	environment := job.Environment
	if environment == nil {
		return names
	}
	if environment.Expression != nil || environment.Name == nil || environment.Name.ContainsExpression() {
		return environmentReferenceNames(nil, nil, nil)
	}
	environments, known := facts.Environments.Value()
	if !known {
		return environmentReferenceNames(nil, nil, nil)
	}
	for _, selected := range environments.Entries() {
		if !strings.EqualFold(selected.Name, environment.Name.Text()) {
			continue
		}
		var variables, secrets []string
		if value, known := selected.Variables.Value(); known {
			variables = value.Names()
		}
		if value, known := selected.Secrets.Value(); known {
			secrets = value.Values()
		}
		return environmentReferenceNames(unionNames(names.Variables, variables), unionNames(names.Secrets, secrets), secrets)
	}
	return names
}
