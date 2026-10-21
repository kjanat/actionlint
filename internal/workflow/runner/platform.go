// Package runner owns runner-label validation, compatibility and platform inference.
package runner

import (
	"slices"
	"strings"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// Platform identifies the runner path and shell conventions known to analysis.
type Platform int

const (
	Any Platform = iota
	MacOrLinux
	Windows
)

// PlatformOf infers a consistent platform from known runner labels.
func PlatformOf(runner *workflowlang.Runner) Platform {
	if runner == nil {
		return Any
	}

	// Note: Labels for self-hosted runners:
	// https://docs.github.com/en/actions/hosting-your-own-runners/using-labels-with-self-hosted-runners

	ret := Any
	for _, label := range Labels(runner) {
		k := Any
		l := strings.ToLower(label.Value)
		if windowsLabel(l) {
			k = Windows
		} else if defaultRunnerOSCompats[l]&(defaultRunnerOSCompats["macos"]|defaultRunnerOSCompats["linux"]) != 0 {
			k = MacOrLinux
		}

		if k == Any {
			continue
		}
		if ret != Any && ret != k {
			// Conflicts are reported by runner-label rule so simply ignore here
			return Any
		}
		ret = k
	}

	return ret
}

// Custom runner names conventionally separate their platform with hyphens.
// A substring such as "windowsish" supplies no platform information.
func windowsLabel(label string) bool {
	return slices.Contains(strings.Split(strings.ToLower(label), "-"), "windows")
}

// Labels resolves static runs-on expressions while preserving dynamic label nodes.
func Labels(runner *workflowlang.Runner) []*workflowlang.String {
	if runner == nil {
		return nil
	}
	var labels []*workflowlang.String
	for _, expression := range []*workflowlang.String{runner.Expression, runner.LabelsExpr} {
		if expression != nil {
			if known, ok := ExpressionLabels(expression); ok {
				labels = append(labels, known...)
			}
		}
	}
	for _, label := range runner.Labels {
		if known, ok := ExpressionLabels(label); ok {
			labels = append(labels, known...)
		} else {
			labels = append(labels, label)
		}
	}
	return labels
}

// Hosted reports whether one explicit hosted runner image is known.
func Hosted(runner *workflowlang.Runner) bool {
	if runner == nil || runner.Group != nil {
		return false
	}
	for _, expression := range []*workflowlang.String{runner.Expression, runner.LabelsExpr} {
		if expression == nil {
			continue
		}
		value, known := workflowlang.WorkflowExpressionLiteral(expression)
		if !known {
			return false
		}
		if selection, ok := value.(map[string]any); ok {
			for key := range selection {
				if !strings.EqualFold(key, "labels") {
					return false
				}
			}
		}
	}
	labels := Labels(runner)
	if len(labels) != 1 {
		return false
	}
	for _, label := range labels {
		if !slices.Contains(allGitHubHostedRunnerLabels, strings.ToLower(label.Value)) {
			return false
		}
	}
	return len(labels) != 0
}
