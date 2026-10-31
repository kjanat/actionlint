package builtin

import (
	"strings"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowexpr "actionlint.kjanat.dev/v2/internal/workflow/expressions"
	"actionlint.kjanat.dev/v2/repository"
)

func environmentReferenceNames(variables, secrets, environmentSecrets []string) workflowexpr.ReferenceNames {
	var extra exprlang.ObjectBuilder
	if environmentSecrets == nil {
		extra.SetMapped(exprlang.StringType())
	}
	for _, name := range environmentSecrets {
		extra.SetProperty(name, exprlang.StringType())
	}
	return workflowexpr.ReferenceNames{Variables: variables, Secrets: secrets, EnvironmentSecrets: new(extra.Build())}
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

func jobReferenceNames(facts repository.Snapshot, job workflowdecl.JobView) workflowexpr.ReferenceNames {
	names := workflowexpr.ReferenceNames{Variables: repoconfig.VariableNames(facts), Secrets: repoconfig.SecretNames(facts)}
	environment := job.Environment()
	if !environment.Present() {
		return names
	}
	if environment.Expression().Present() || !environment.Name().Present() || environment.Name().ContainsExpression() {
		return environmentReferenceNames(nil, nil, nil)
	}
	environments, known := facts.Environments.Value()
	if !known {
		return environmentReferenceNames(nil, nil, nil)
	}
	for _, selected := range environments.Entries() {
		if !strings.EqualFold(selected.Name, environment.Name().Value()) {
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
