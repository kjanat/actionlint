package expressions

import (
	"strings"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	workflowexpr "actionlint.kjanat.dev/v2/internal/checking/expressions"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"

	repository "actionlint.kjanat.dev/v2/internal/model/repositories"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func environmentReferenceNames(variables, secrets, environmentSecrets []string) workflowexpr.ReferenceNames {
	var extra exprtypes.ObjectBuilder
	if environmentSecrets == nil {
		extra.SetMapped(exprtypes.StringType())
	}
	for _, name := range environmentSecrets {
		extra.SetProperty(name, exprtypes.StringType())
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
