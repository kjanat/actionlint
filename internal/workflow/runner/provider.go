package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/workflow/value"
	runnerapi "actionlint.kjanat.dev/v2/runner"
)

type builtinProvider struct{ custom []string }

var builtinDataset = func() string {
	data, _ := json.Marshal(struct {
		Version                       string
		Hosted, OSLabels, OtherLabels []string
		Compatibility                 map[string]runnerOSCompat
	}{"5", allGitHubHostedRunnerLabels, selfHostedRunnerPresetOSLabels, selfHostedRunnerPresetOtherLabels, defaultRunnerOSCompats})
	return fmt.Sprintf("github:%x", sha256.Sum256(data))
}()

func (builtinProvider) RunnerDescriptor() runnerapi.Descriptor {
	return runnerapi.Descriptor{Name: "github", Version: "1", Targets: []string{"github"}}
}

func (p builtinProvider) ResolveRunners(ctx context.Context, request runnerapi.Request) ([]runnerapi.Answer, error) {
	answers := make([]runnerapi.Answer, 0, len(request.Selectors))
	for _, selector := range request.Selectors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		answer := runnerapi.Answer{Evidence: runnerapi.Evidence{Dataset: builtinDataset, Scope: request.Scope, Freshness: runnerapi.Fresh, Complete: true}}
		if request.Target != "github" {
			answer.Resolution = runnerapi.NotApplicable{}
			answers = append(answers, answer)
			continue
		}
		check := checker{labels: p.custom, compats: map[runnerOSCompat]labelFact{}}
		unknown := selector.Expression != "" || selector.Incomplete
		if selector.Group != nil {
			_, unresolved := selector.Group.Value.(runnerapi.Expression)
			unknown = unknown || unresolved
		}
		var labels []value.Value
		for _, constraint := range selector.Labels {
			switch term := constraint.Value.(type) {
			case runnerapi.Literal:
				item := value.FromJSON(string(term), value.Origin{Span: value.Span{Start: constraint.Position}})
				labels = append(labels, item)
				fact, _ := knownLabel(item)
				check.checkCompat(check.verifyRunnerLabel(fact), fact)
			case runnerapi.Expression:
				unknown = true
			}
		}
		switch {
		case len(check.rejections) != 0:
			answer.Resolution = runnerapi.NoMatch{Rejections: check.rejections}
		case unknown:
			answer.Resolution = runnerapi.Unknown{Reason: "runner selector depends on runtime values"}
		default:
			selection := value.Map(value.Origin{}, value.Prop{Name: "labels", Value: value.Seq(value.Origin{}, labels...)})
			if selector.Group != nil {
				selection = value.Map(value.Origin{}, value.Prop{Name: "labels", Value: value.Seq(value.Origin{}, labels...)}, value.Prop{Name: "group", Value: value.FromJSON("group", value.Origin{})})
			}
			knowledge := builtinKnowledge(selection, labels)
			candidate := runnerapi.Candidate{Name: "github", OS: runnerapi.UnknownOS, DefaultShell: knowledge.Shell, CaseSensitivity: runnerapi.UnknownCase, ShIsDash: knowledge.ShIsDash, Ephemeral: knowledge.Ephemeral}
			if knowledge.PathsKnown {
				candidate.CaseSensitivity = runnerapi.CaseSensitive
				if knowledge.CaseInsensitive {
					candidate.CaseSensitivity = runnerapi.CaseInsensitive
				}
			}
			switch knowledge.Platform {
			case Windows:
				candidate.OS = runnerapi.Windows
			case MacOrLinux:
				candidate.OS = runnerapi.Linux
				for _, label := range labels {
					name, _ := label.ScalarString()
					if defaultRunnerOSCompats[strings.ToLower(name)]&defaultRunnerOSCompats["macos"] != 0 {
						candidate.OS = runnerapi.MacOS
					}
				}
			}
			answer.Resolution = runnerapi.Resolved{Candidates: []runnerapi.Candidate{candidate}}
		}
		answers = append(answers, answer)
	}
	return answers, nil
}
