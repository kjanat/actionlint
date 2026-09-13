package actionlint

import (
	"maps"
	"path"
	"strconv"
	"strings"
)

type runnerOSCompat uint

const (
	compatInvalid                   = 0
	compatUbuntu2204 runnerOSCompat = 1 << iota
	compatUbuntu2404
	compatUbuntu2604
	compatXcode270
	compatXcode270XL
	compatMacOS140
	compatMacOS140L
	compatMacOS140XL
	compatMacOS150
	compatMacOS150Intel
	compatMacOS150L
	compatMacOS150XL
	compatMacOS260
	compatMacOS260Intel
	compatMacOS260L
	compatMacOS260XL
	compatWindows2022
	compatWindows2025VS2026
	compatWindows11Arm
	compatWindows11VS2026Arm
)

// https://docs.github.com/en/actions/using-github-hosted-runners/about-github-hosted-runners
var allGitHubHostedRunnerLabels = []string{
	"windows-latest",
	"windows-latest-8-cores",
	"windows-2025",
	"windows-2025-vs2026",
	"windows-2022",
	"windows-11-arm",
	"windows-11-vs2026-arm",
	"ubuntu-slim",
	"ubuntu-latest",
	"ubuntu-latest-4-cores",
	"ubuntu-latest-8-cores",
	"ubuntu-latest-16-cores",
	"ubuntu-26.04",
	"ubuntu-26.04-arm",
	"ubuntu-24.04",
	"ubuntu-24.04-arm",
	"ubuntu-22.04",
	"ubuntu-22.04-arm",
	"xcode-27",
	"xcode-27-xlarge",
	"macos-latest",
	"macos-latest-xlarge",
	"macos-latest-large",
	"macos-26-intel",
	"macos-26-xlarge",
	"macos-26-large",
	"macos-26",
	"macos-15-intel",
	"macos-15-xlarge",
	"macos-15-large",
	"macos-15",
	"macos-14-xlarge",
	"macos-14-large",
	"macos-14",
}

// https://docs.github.com/en/actions/hosting-your-own-runners/using-self-hosted-runners-in-a-workflow#using-default-labels-to-route-jobs
var selfHostedRunnerPresetOSLabels = []string{
	"linux",
	"macos",
	"windows",
}

// https://docs.github.com/en/actions/hosting-your-own-runners/using-self-hosted-runners-in-a-workflow#using-default-labels-to-route-jobs
var selfHostedRunnerPresetOtherLabels = []string{
	"self-hosted",
	"x64",
	"arm",
	"arm64",
}

var defaultRunnerOSCompats = map[string]runnerOSCompat{
	"ubuntu-slim":            compatUbuntu2404,
	"ubuntu-latest":          compatUbuntu2404,
	"ubuntu-latest-4-cores":  compatUbuntu2404,
	"ubuntu-latest-8-cores":  compatUbuntu2404,
	"ubuntu-latest-16-cores": compatUbuntu2404,
	"ubuntu-26.04":           compatUbuntu2604,
	"ubuntu-26.04-arm":       compatUbuntu2604,
	"ubuntu-24.04":           compatUbuntu2404,
	"ubuntu-24.04-arm":       compatUbuntu2404,
	"ubuntu-22.04":           compatUbuntu2204,
	"ubuntu-22.04-arm":       compatUbuntu2204,
	"xcode-27":               compatXcode270,
	"xcode-27-xlarge":        compatXcode270XL,
	"macos-latest-xlarge":    compatMacOS150XL,
	"macos-latest-large":     compatMacOS260L,
	"macos-latest":           compatMacOS260,
	"macos-26-intel":         compatMacOS260Intel,
	"macos-26-xlarge":        compatMacOS260XL,
	"macos-26-large":         compatMacOS260L,
	"macos-26":               compatMacOS260,
	"macos-15-intel":         compatMacOS150Intel,
	"macos-15-xlarge":        compatMacOS150XL,
	"macos-15-large":         compatMacOS150L,
	"macos-15":               compatMacOS150,
	"macos-14-xlarge":        compatMacOS140XL,
	"macos-14-large":         compatMacOS140L,
	"macos-14":               compatMacOS140,
	"windows-latest":         compatWindows2025VS2026,
	"windows-latest-8-cores": compatWindows2022,
	"windows-2025":           compatWindows2025VS2026,
	"windows-2025-vs2026":    compatWindows2025VS2026,
	"windows-2022":           compatWindows2022,
	// The alias migrates from VS2022 to VS2026 during September 21-30, 2026.
	// Allow both images while GitHub rolls the alias forward.
	"windows-11-arm":        compatWindows11Arm | compatWindows11VS2026Arm,
	"windows-11-vs2026-arm": compatWindows11VS2026Arm,
	"linux":                 compatUbuntu2604 | compatUbuntu2404 | compatUbuntu2204, // Note: "linux" does not always indicate Ubuntu. It might be Fedora or Arch or ...
	"macos":                 compatXcode270 | compatXcode270XL | compatMacOS260 | compatMacOS260Intel | compatMacOS260L | compatMacOS260XL | compatMacOS150 | compatMacOS150Intel | compatMacOS150L | compatMacOS150XL | compatMacOS140 | compatMacOS140L | compatMacOS140XL,
	"windows":               compatWindows2025VS2026 | compatWindows2022 | compatWindows11Arm | compatWindows11VS2026Arm,
}

// RuleRunnerLabel is a rule to check runner label like "ubuntu-latest". There are two types of
// runners, GitHub-hosted runner and Self-hosted runner. GitHub-hosted runner is described at
// https://docs.github.com/en/actions/using-github-hosted-runners/about-github-hosted-runners .
// And Self-hosted runner is described at
// https://docs.github.com/en/actions/hosting-your-own-runners/using-self-hosted-runners-in-a-workflow .
type RuleRunnerLabel struct {
	RuleBase
	// Note: Using only one compatibility integer is enough to check compatibility. But we remember
	// all past compatibility values here for better error message. If accumulating all compatibility
	// values into one integer, we can no longer know what labels are conflicting.
	compats map[runnerOSCompat]*String
}

// NewRuleRunnerLabel creates new RuleRunnerLabel instance.
func NewRuleRunnerLabel() *RuleRunnerLabel {
	return &RuleRunnerLabel{
		RuleBase: RuleBase{
			name: "runner-label",
			desc: "Checks for GitHub-hosted and preset self-hosted runner labels in \"runs-on:\"",
		},
		compats: nil,
	}
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleRunnerLabel) VisitJobPre(n *Job) error {
	if n.RunsOn == nil {
		return nil
	}

	if len(n.RunsOn.Labels) == 1 {
		rule.checkLabel(n.RunsOn.Labels[0], n.Strategy)
		return nil
	}

	rule.compats = map[runnerOSCompat]*String{}
	switch {
	case n.RunsOn.Expression != nil:
		rule.checkLabelAndConflict(n.RunsOn.Expression, n.Strategy)
	case n.RunsOn.LabelsExpr != nil:
		rule.checkLabelAndConflict(n.RunsOn.LabelsExpr, n.Strategy)
	default:
		for _, label := range n.RunsOn.Labels {
			rule.checkLabelAndConflict(label, n.Strategy)
		}
	}

	rule.compats = nil // reset
	return nil
}

// https://docs.github.com/en/actions/using-github-hosted-runners/about-github-hosted-runners
func (rule *RuleRunnerLabel) checkLabelAndConflict(l *String, strategy *Strategy) {
	if l.ContainsExpression() {
		if labels, known := knownRunnerExpressionLabels(l); known {
			for _, label := range labels {
				rule.checkCompat(rule.verifyRunnerLabel(label), label)
			}
			return
		}
		rule.checkMatrixLabels(rule.tryToGetLabelsInMatrix(l, strategy))
		return
	}

	comp := rule.verifyRunnerLabel(l)
	rule.checkCompat(comp, l)
}

func (rule *RuleRunnerLabel) checkLabel(l *String, strategy *Strategy) {
	if l.ContainsExpression() {
		if labels, known := knownRunnerExpressionLabels(l); known {
			for _, label := range labels {
				rule.verifyRunnerLabel(label)
			}
			return
		}
		for _, labels := range rule.tryToGetLabelsInMatrix(l, strategy) {
			for _, label := range labels {
				rule.verifyRunnerLabel(label)
			}
		}
		return
	}

	rule.verifyRunnerLabel(l)
}

func knownRunnerExpressionLabels(expr *String) ([]*String, bool) {
	value, known := workflowExpressionLiteral(expr)
	if !known {
		return nil, false
	}
	return runnerExpressionLabels(value, expr.Pos, false), true
}

func runnerExpressionLabels(value any, pos *Pos, nullIsLabel bool) []*String {
	if object, ok := value.(map[string]any); ok {
		value = nil
		found := false
		for name, field := range object {
			if strings.EqualFold(name, "labels") {
				value, found = field, true
				break
			}
		}
		if !found {
			return nil
		}
	}
	var labels []*String
	appendLabel := func(value any) {
		if value == nil && !nullIsLabel {
			return // The runs-on schema reports null values.
		}
		if label := runnerExpressionLabel(value, pos); label != nil {
			labels = append(labels, label)
		}
	}
	if array, ok := value.([]any); ok {
		for _, item := range array {
			appendLabel(item)
		}
	} else {
		appendLabel(value)
	}
	return labels
}

func runnerExpressionLabel(value any, pos *Pos) *String {
	var label string
	switch value := value.(type) {
	case nil:
		label = ""
	case string:
		label = value
	case bool:
		label = strconv.FormatBool(value)
	case float64:
		label = strconv.FormatFloat(value, 'g', -1, 64)
	default:
		return nil
	}
	return &String{Value: label, Pos: pos}
}

func knownStrategyRunnerLabels(expr *String, property string) [][]*String {
	value, known := workflowExpressionLiteral(expr)
	if !known {
		return nil
	}
	strategy, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	matrix, ok := workflowObjectProperty(strategy, "matrix").(map[string]any)
	if !ok {
		return nil
	}
	var labels [][]*String
	if row, ok := workflowObjectProperty(matrix, property).([]any); ok {
		for _, value := range row {
			labels = append(labels, runnerExpressionLabels(value, expr.Pos, true))
		}
	}
	if include, ok := workflowObjectProperty(matrix, "include").([]any); ok {
		for _, value := range include {
			if assignments, ok := value.(map[string]any); ok {
				for name, value := range assignments {
					if strings.EqualFold(name, property) {
						labels = append(labels, runnerExpressionLabels(value, expr.Pos, true))
						break
					}
				}
			}
		}
	}
	return labels
}

func (rule *RuleRunnerLabel) verifyRunnerLabel(label *String) runnerOSCompat {
	l := label.Value
	if c, ok := defaultRunnerOSCompats[strings.ToLower(l)]; ok {
		return c
	}

	for _, p := range selfHostedRunnerPresetOtherLabels {
		if strings.EqualFold(l, p) {
			return compatInvalid
		}
	}

	known := rule.getKnownLabels()
	for _, k := range known {
		m, err := path.Match(k, l)
		if err != nil {
			rule.Errorf(label.Pos, "label pattern %q is an invalid glob. kindly check list of labels in actionlint.yaml config file: %v", k, err)
			return compatInvalid
		}
		if m {
			return compatInvalid
		}
	}

	rule.Errorf(
		label.Pos,
		"label %q is unknown. available labels are %s. if it is a custom label for self-hosted runner, set list of labels in actionlint.yaml config file",
		label.Value,
		quotesAll(
			allGitHubHostedRunnerLabels,
			selfHostedRunnerPresetOtherLabels,
			selfHostedRunnerPresetOSLabels,
			known,
		),
	)

	return compatInvalid
}

func (rule *RuleRunnerLabel) tryToGetLabelsInMatrix(label *String, strategy *Strategy) [][]*String {
	if strategy == nil {
		return nil
	}

	deref, ok := parseAssignedExpression(label.Value).(*ObjectDerefNode)
	if !ok {
		return nil
	}
	recv, ok := deref.Receiver.(*VariableNode)
	if !ok {
		return nil
	}
	if recv.Name != "matrix" {
		return nil
	}

	prop := deref.Property
	if strategy.Expression != nil {
		return knownStrategyRunnerLabels(strategy.Expression, prop)
	}
	m := strategy.Matrix
	if m == nil {
		return nil
	}
	var labels [][]*String

	if m.Rows != nil {
		if row, ok := m.Rows[prop]; ok {
			for _, v := range row.Values {
				if s, ok := v.(*RawYAMLString); ok && !ContainsExpression(s.Value) {
					labels = append(labels, []*String{{s.Value, false, s.Pos()}})
				}
			}
		}
	}

	if m.Include != nil {
		for _, combi := range m.Include.Combinations {
			if combi.Assigns != nil {
				if assign, ok := combi.Assigns[prop]; ok {
					if s, ok := assign.Value.(*RawYAMLString); ok && !ContainsExpression(s.Value) {
						labels = append(labels, []*String{{s.Value, false, s.Pos()}})
					}
				}
			}
		}
	}

	return labels
}

func (rule *RuleRunnerLabel) checkConflict(comp runnerOSCompat, label *String) bool {
	for c, l := range rule.compats {
		if c&comp == 0 {
			rule.Errorf(label.Pos, "label %q conflicts with label %q defined at %s. note: to run your job on each workers, use matrix", label.Value, l.Value, l.Pos)
			return false
		}
	}
	return true
}

func (rule *RuleRunnerLabel) checkCompat(comp runnerOSCompat, label *String) {
	if comp == compatInvalid || !rule.checkConflict(comp, label) {
		return
	}
	if _, ok := rule.compats[comp]; !ok {
		rule.compats[comp] = label
	}
}

func (rule *RuleRunnerLabel) checkMatrixLabels(groups [][]*String) {
	previous := rule.compats
	combined := maps.Clone(previous)
	for _, labels := range groups {
		rule.compats = maps.Clone(previous)
		for _, label := range labels {
			rule.checkCompat(rule.verifyRunnerLabel(label), label)
		}
		for compat, label := range rule.compats {
			if _, exists := combined[compat]; !exists {
				combined[compat] = label
			}
		}
	}
	rule.compats = combined
}

func (rule *RuleRunnerLabel) getKnownLabels() []string {
	if rule.config == nil {
		return nil
	}
	return rule.config.SelfHostedRunner.Labels
}
