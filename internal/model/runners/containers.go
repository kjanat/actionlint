package runners

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/model/contexts/value"

	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"

	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/matrix"
)

type ContainerLimitation uint8

const (
	ContainerUnknown ContainerLimitation = iota
	ContainerRequiresLinux
	ContainerMissingDaemon
)

type ContainerTarget struct {
	Limitation  ContainerLimitation
	Runner      string
	OS          runnerapi.OS
	Matrix      matrix.Combination
	MatrixKnown bool
}

// ContainerLimitations never infers an external provider's capabilities from its label spelling.
func (k *Knowledge) ContainerLimitations(job workflows.BoundJob) []ContainerTarget {
	if k == nil || k.report.Target != "github" {
		return nil
	}
	if job.Strategy != nil && job.Strategy.Matrix != nil {
		if rows, complete := workflows.MatrixResolution(job.Strategy).Combinations(256); complete && len(rows) == 0 {
			return nil
		}
	}
	alternatives := Alternatives(job)
	answers := k.Job(job).Answers
	if len(selectorAlternatives(job)) != len(answers) {
		return nil
	}
	var targets []ContainerTarget
	for _, alternative := range alternatives {
		answer := answers[alternative.AnswerIndex]
		if alternative.Selector.Incomplete || !answer.DatasetStatus.Complete || answer.DatasetStatus.Freshness != runnerapi.Fresh {
			continue
		}
		resolved, ok := answer.Resolution.(runnerapi.Resolved)
		if !ok || len(resolved.Candidates) == 0 {
			continue
		}
		var labels []string
		slim := false
		for _, constraint := range alternative.Selector.Labels {
			if label, literal := constraint.Value.(runnerapi.Literal); literal {
				labels = append(labels, string(label))
				slim = slim || strings.EqualFold(string(label), "ubuntu-slim")
			}
		}
		slim = slim && answer.DatasetStatus.DatasetID == builtinDataset
		unsupported := true
		for _, candidate := range resolved.Candidates {
			unsupported = unsupported && (candidate.OS == runnerapi.Windows || candidate.OS == runnerapi.MacOS || slim)
		}
		if unsupported {
			for _, candidate := range resolved.Candidates {
				limitation := ContainerRequiresLinux
				if slim {
					limitation = ContainerMissingDaemon
				}
				name := strings.Join(labels, ", ")
				if name == "" {
					name = candidate.Name
				}
				if name == "" {
					name = string(candidate.OS)
				}
				targets = append(targets, ContainerTarget{Limitation: limitation, Runner: name, OS: candidate.OS, Matrix: alternative.Matrix, MatrixKnown: alternative.MatrixKnown})
			}
		}
	}
	return targets
}

// LookupExpressionVariable supplies no runner object when the provider cannot establish its OS.
func (target ContainerTarget) LookupExpressionVariable(name string) (any, bool) {
	if name == "runner.os" {
		var os string
		switch target.OS {
		case runnerapi.Linux:
			os = "Linux"
		case runnerapi.MacOS:
			os = "macOS"
		case runnerapi.Windows:
			os = "Windows"
		case runnerapi.UnknownOS:
			return nil, false
		}
		return os, true
	}
	if target.MatrixKnown {
		return target.Matrix.LookupExpressionVariable(name)
	}
	return nil, false
}

func (target ContainerTarget) Evaluate(text string) value.Value {
	node := syntax.ParseAssignedExpression(text)
	if node == nil {
		return value.Template(text, value.Origin{})
	}
	if result, known := evaluate.ExpressionValueWithLookup(node, target.LookupExpressionVariable); known {
		return value.FromJSON(result, value.Origin{FromExpression: true})
	}
	return value.DynamicValue(text, value.KindUnknown, value.Origin{FromExpression: true})
}
