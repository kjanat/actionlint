package runners

import (
	"context"
	"encoding/json"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
)

type builtinProvider struct{ custom LabelSet }

// GitHubProvider evaluates selectors using built-in labels and supplied custom labels.
func GitHubProvider(custom LabelSet) runnerapi.Provider { return builtinProvider{custom: custom} }

// githubHostedArchitectures records the documented CPU architecture for each
// hosted label. Custom label spelling never supplies an architecture.
var githubHostedArchitectures = map[string]string{
	"ubuntu-slim": "x64", "ubuntu-latest": "x64", "ubuntu-latest-4-cores": "x64", "ubuntu-latest-8-cores": "x64", "ubuntu-latest-16-cores": "x64",
	"ubuntu-22.04": "x64", "ubuntu-24.04": "x64", "ubuntu-26.04": "x64",
	"ubuntu-22.04-arm": "arm64", "ubuntu-24.04-arm": "arm64", "ubuntu-26.04-arm": "arm64",
	"windows-latest": "x64", "windows-latest-8-cores": "x64", "windows-2022": "x64", "windows-2025": "x64", "windows-2025-vs2026": "x64",
	"windows-11-arm": "arm64", "windows-11-vs2026-arm": "arm64",
	"macos-latest": "arm64", "macos-15": "arm64", "macos-26": "arm64", "xcode-27": "arm64",
	"macos-15-intel": "x64", "macos-26-intel": "x64", "macos-latest-large": "x64", "macos-15-large": "x64", "macos-26-large": "x64",
	"macos-latest-xlarge": "arm64", "macos-15-xlarge": "arm64", "macos-26-xlarge": "arm64", "xcode-27-xlarge": "arm64",
}

var builtinDataset = func() string {
	data, _ := json.Marshal(struct {
		Version                       string
		Hosted, OSLabels, OtherLabels []string
		Compatibility                 map[string]runnerOSCompat
		Architectures                 map[string]string
	}{"6", allGitHubHostedRunnerLabels, selfHostedRunnerPresetOSLabels, selfHostedRunnerPresetOtherLabels, defaultRunnerOSCompats, githubHostedArchitectures})
	return "github:" + sha256hex.Digest(data)
}()

func (builtinProvider) RunnerDescriptor() runnerapi.Descriptor {
	return runnerapi.Descriptor{Name: "github", Version: "1", Targets: []string{"github"}}
}

func (p builtinProvider) ResolveRunners(ctx context.Context, request runnerapi.Request) (runnerapi.Response, error) {
	answers := make([]runnerapi.Answer, 0, len(request.Selectors))
	for _, selector := range request.Selectors {
		if err := ctx.Err(); err != nil {
			return runnerapi.Response{}, err
		}
		answer := runnerapi.Answer{DatasetStatus: runnerapi.DatasetStatus{DatasetID: builtinDataset, Scope: request.Scope, Freshness: runnerapi.Fresh, Complete: true}}
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
			if traits.Ephemeral {
				candidate.Hosting = runnerapi.Hosted
			}
			for _, label := range labels {
				if name, known := label.ScalarString(); known && strings.EqualFold(name, "self-hosted") {
					candidate.Hosting = runnerapi.SelfHosted
				}
			}
			var architecture string
			conflicting := false
			for _, label := range labels {
				name, known := label.ScalarString()
				if !known {
					continue
				}
				name = strings.ToLower(name)
				var next string
				if candidate.Hosting != runnerapi.SelfHosted {
					next = githubHostedArchitectures[name]
				} else {
					switch name {
					case "arm":
						next = "arm32"
					case "arm64":
						next = "arm64"
					case "x64":
						next = "x64"
					}
				}
				if next != "" {
					if architecture != "" && architecture != next {
						conflicting = true
					}
					architecture = next
				}
			}
			if !conflicting {
				candidate.Architecture = architecture
			}
			answer.Resolution = runnerapi.Resolved{Candidates: []runnerapi.Candidate{candidate}}
		}
		answers = append(answers, answer)
	}
	return runnerapi.Response{Answers: answers, Support: builtinSupport(request.Scope)}, nil
}
