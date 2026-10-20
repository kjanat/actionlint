package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	runnerapi "actionlint.kjanat.dev/v2/runner"
)

// builtinProvider implements the same boundary as external providers. The
// catalogue validator returns constraint rejections, never findings.
type builtinProvider struct{ custom []string }

// Include catalogue contents and interpretation version in dependent identities.
var builtinDataset = func() string {
	data, _ := json.Marshal(struct {
		Version                       string
		Hosted, OSLabels, OtherLabels []string
		Compatibility                 map[string]runnerOSCompat
	}{"1", allGitHubHostedRunnerLabels, selfHostedRunnerPresetOSLabels, selfHostedRunnerPresetOtherLabels, defaultRunnerOSCompats})
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
		check := checker{labels: p.custom, compats: map[runnerOSCompat]*workflowlang.String{}}
		var labels []*workflowlang.String
		unknown := selector.Expression != "" || selector.Incomplete
		if selector.Group != nil {
			_, unresolved := selector.Group.Value.(runnerapi.Expression)
			unknown = unknown || unresolved
		}
		for _, constraint := range selector.Labels {
			switch value := constraint.Value.(type) {
			case runnerapi.Literal:
				label := &workflowlang.String{Value: string(value), Pos: &workflowlang.Pos{Line: constraint.Position.Line, Col: constraint.Position.Column}}
				labels = append(labels, label)
				check.checkCompat(check.verifyRunnerLabel(label), label)
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
			r := &workflowlang.Runner{Labels: labels}
			if selector.Group != nil {
				r.Group = &workflowlang.String{Value: "group"}
			}
			k := builtinJob(&workflowlang.Job{RunsOn: r})
			candidate := runnerapi.Candidate{Name: "github", OS: runnerapi.UnknownOS, DefaultShell: k.Shell, CaseSensitivity: runnerapi.UnknownCase, ShIsDash: k.ShIsDash, Ephemeral: k.Ephemeral}
			if k.PathsKnown {
				candidate.CaseSensitivity = runnerapi.CaseSensitive
				if k.CaseInsensitive {
					candidate.CaseSensitivity = runnerapi.CaseInsensitive
				}
			}
			switch k.Platform {
			case Windows:
				candidate.OS = runnerapi.Windows
			case MacOrLinux:
				candidate.OS = runnerapi.Linux
				for _, label := range labels {
					if strings.HasPrefix(strings.ToLower(label.Value), "macos") || strings.HasPrefix(strings.ToLower(label.Value), "xcode-") {
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
