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

type builtinProvider struct{ custom LabelSet }

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
		check := checker{labels: p.custom.patterns, labelsKnown: p.custom.known}
		labels, unknown := check.checkSelector(selector)
		switch {
		case len(check.rejections) != 0:
			answer.Resolution = runnerapi.NoMatch{Rejections: check.rejections}
		case unknown:
			answer.Resolution = runnerapi.Unknown{Reason: "runner selector depends on runtime values"}
			if check.unknownLabels {
				answer.Resolution = runnerapi.Unknown{Reason: "repository runner labels are unavailable"}
			}
		default:
			selection := value.Map(value.Origin{}, value.Prop{Name: "labels", Value: value.Seq(value.Origin{}, labels...)})
			if selector.Group != nil {
				selection = value.Map(value.Origin{}, value.Prop{Name: "labels", Value: value.Seq(value.Origin{}, labels...)}, value.Prop{Name: "group", Value: value.FromJSON("group", value.Origin{})})
			}
			traits := builtinTraits(selection, labels)
			candidate := runnerapi.Candidate{Name: "github", OS: runnerapi.UnknownOS, DefaultShell: traits.Shell, CaseSensitivity: runnerapi.UnknownCase, ShIsDash: traits.ShIsDash, Ephemeral: traits.Ephemeral}
			if traits.PathsKnown {
				candidate.CaseSensitivity = runnerapi.CaseSensitive
				if traits.CaseInsensitive {
					candidate.CaseSensitivity = runnerapi.CaseInsensitive
				}
			}
			switch traits.Platform {
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
			case Any:
			}
			answer.Resolution = runnerapi.Resolved{Candidates: []runnerapi.Candidate{candidate}}
		}
		answers = append(answers, answer)
	}
	return answers, nil
}
