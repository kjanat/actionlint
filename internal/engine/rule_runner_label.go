package engine

import (
	"maps"
	"path"
	"slices"
	"strings"

	"actionlint.kjanat.dev/internal/workflow/schema"

	exprlang "actionlint.kjanat.dev/internal/expression"
	stringutil "actionlint.kjanat.dev/internal/stringutil"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

type runnerOSCompat uint

const (
	compatInvalid                   = 0
	compatUbuntu2204 runnerOSCompat = 1 << iota
	compatUbuntu2404
	compatUbuntu2604
	compatUbuntu2204ARM
	compatUbuntu2404ARM
	compatUbuntu2604ARM
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
	"ubuntu-slim": compatUbuntu2404,
	// GitHub migrates this alias during October 19-November 19, 2026.
	// Both images can serve the alias during the rollout.
	"ubuntu-latest":          compatUbuntu2404 | compatUbuntu2604,
	"ubuntu-latest-4-cores":  compatUbuntu2404,
	"ubuntu-latest-8-cores":  compatUbuntu2404,
	"ubuntu-latest-16-cores": compatUbuntu2404,
	"ubuntu-26.04":           compatUbuntu2604,
	"ubuntu-26.04-arm":       compatUbuntu2604ARM,
	"ubuntu-24.04":           compatUbuntu2404,
	"ubuntu-24.04-arm":       compatUbuntu2404ARM,
	"ubuntu-22.04":           compatUbuntu2204,
	"ubuntu-22.04-arm":       compatUbuntu2204ARM,
	"xcode-27":               compatXcode270,
	"xcode-27-xlarge":        compatXcode270XL,
	"macos-latest-xlarge":    compatMacOS260XL,
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
	"linux":                 compatUbuntu2604 | compatUbuntu2404 | compatUbuntu2204 | compatUbuntu2604ARM | compatUbuntu2404ARM | compatUbuntu2204ARM, // Note: "linux" does not always indicate Ubuntu. It might be Fedora or Arch or ...
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
	ruleConfiguration
	// Note: Using only one compatibility integer is enough to check compatibility. But we remember
	// all past compatibility values here for better error message. If accumulating all compatibility
	// values into one integer, we can no longer know what labels are conflicting.
	compats map[runnerOSCompat]*workflowlang.String
}

// NewRuleRunnerLabel creates new RuleRunnerLabel instance.
func NewRuleRunnerLabel() *RuleRunnerLabel {
	return &RuleRunnerLabel{
		RuleBase: builtinRuleBase("runner-label"),
		compats:  nil,
	}
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleRunnerLabel) VisitJobPre(n *workflowlang.Job) error {
	if n.RunsOn == nil {
		return nil
	}
	if rule.checkCorrelatedMatrixLabels(n) {
		return nil
	}

	if len(n.RunsOn.Labels) == 1 {
		rule.checkLabel(n.RunsOn.Labels[0], n.Strategy)
		return nil
	}

	rule.compats = map[runnerOSCompat]*workflowlang.String{}
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
func (rule *RuleRunnerLabel) checkLabelAndConflict(l *workflowlang.String, strategy *workflowlang.Strategy) {
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

func (rule *RuleRunnerLabel) checkLabel(l *workflowlang.String, strategy *workflowlang.Strategy) {
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

func knownRunnerExpressionLabels(expr *workflowlang.String) ([]*workflowlang.String, bool) {
	value, known := workflowlang.WorkflowExpressionLiteral(expr)
	if !known {
		return nil, false
	}
	return runnerExpressionLabels(value, expr.Pos, false), true
}

func runnerExpressionLabels(value any, pos *workflowlang.Pos, nullIsLabel bool) []*workflowlang.String {
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
	var labels []*workflowlang.String
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

func runnerExpressionLabel(value any, pos *workflowlang.Pos) *workflowlang.String {
	label, ok := workflowlang.WorkflowScalarString(value)
	if !ok {
		return nil
	}
	return &workflowlang.String{Value: label, Pos: pos}
}

func knownStrategyRunnerLabels(expr *workflowlang.String, property string) [][]*workflowlang.String {
	value, known := workflowlang.WorkflowExpressionLiteral(expr)
	if !known {
		return nil
	}
	strategy, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	matrix, ok := schema.Property(strategy, "matrix").(map[string]any)
	if !ok {
		return nil
	}
	var labels [][]*workflowlang.String
	if row, ok := schema.Property(matrix, property).([]any); ok {
		for _, value := range row {
			labels = append(labels, runnerExpressionLabels(value, expr.Pos, true))
		}
	}
	if include, ok := schema.Property(matrix, "include").([]any); ok {
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

func (rule *RuleRunnerLabel) verifyRunnerLabel(label *workflowlang.String) runnerOSCompat {
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
		stringutil.QuotesAll(
			allGitHubHostedRunnerLabels,
			selfHostedRunnerPresetOtherLabels,
			selfHostedRunnerPresetOSLabels,
			known,
		),
	)

	return compatInvalid
}

func (rule *RuleRunnerLabel) tryToGetLabelsInMatrix(label *workflowlang.String, strategy *workflowlang.Strategy) [][]*workflowlang.String {
	if strategy == nil {
		return nil
	}
	prop := runnerMatrixProperty(label)
	if prop == "" {
		return nil
	}
	if strategy.Expression != nil {
		return knownStrategyRunnerLabels(strategy.Expression, prop)
	}
	m := strategy.Matrix
	if m != nil && len(m.Insertions) != 0 {
		m, _ = expandedMatrixInsertions(m)
	}
	if m == nil {
		return nil
	}
	var labels [][]*workflowlang.String

	if row := m.Rows[prop]; row != nil {
		elements, _ := matrixRowElements(row, true)
		for _, element := range elements {
			if scalar, ok := element.value.(*workflowlang.RawYAMLString); ok && (!element.expressions || !exprlang.ContainsExpression(scalar.Value)) {
				labels = append(labels, []*workflowlang.String{{Value: scalar.Value, Quoted: false, Pos: scalar.Pos()}})
			}
		}
	}
	for _, combination := range matrixExcludeCombinations(m.Include, true) {
		if assign := combination.combination.Assigns[prop]; assign != nil {
			if scalar, ok := assign.Value.(*workflowlang.RawYAMLString); ok && (!combination.expressions || !exprlang.ContainsExpression(scalar.Value)) {
				labels = append(labels, []*workflowlang.String{{Value: scalar.Value, Quoted: false, Pos: scalar.Pos()}})
			}
		}
	}

	return labels
}

func runnerMatrixProperty(label *workflowlang.String) string {
	deref, ok := exprlang.ParseAssignedExpression(label.Value).(*exprlang.ObjectDerefNode)
	if !ok {
		return ""
	}
	if recv, ok := deref.Receiver.(*exprlang.VariableNode); ok && recv.Name == "matrix" {
		return deref.Property
	}
	return ""
}

func (rule *RuleRunnerLabel) checkCorrelatedMatrixLabels(job *workflowlang.Job) bool {
	properties := make([]string, len(job.RunsOn.Labels))
	references := 0
	for i, label := range job.RunsOn.Labels {
		properties[i] = runnerMatrixProperty(label)
		if properties[i] != "" {
			references++
		}
	}
	if references < 2 {
		return false
	}

	rule.compats = map[runnerOSCompat]*workflowlang.String{}
	defer func() { rule.compats = nil }()
	checkLabels := func(labels []*workflowlang.String) {
		for _, label := range labels {
			rule.checkCompat(defaultRunnerOSCompats[strings.ToLower(label.Value)], label)
		}
	}
	for i, label := range job.RunsOn.Labels {
		rule.checkLabel(label, job.Strategy)
		if properties[i] == "" {
			if labels, known := knownRunnerExpressionLabels(label); known {
				checkLabels(labels)
			} else if !label.ContainsExpression() {
				checkLabels([]*workflowlang.String{label})
			}
		}
	}
	fixed := rule.compats
	if combinations, known := knownRunnerMatrixCombinations(job.Strategy); known {
		for _, combination := range combinations {
			rule.compats = maps.Clone(fixed)
			for _, property := range properties {
				if value, ok := combination[property]; ok {
					checkLabels(runnerMatrixValueLabels(value))
				}
			}
		}
	} else {
		// Unknown combinations retain checks against fixed labels and within each value.
		for i, label := range job.RunsOn.Labels {
			if properties[i] == "" {
				continue
			}
			for _, labels := range rule.tryToGetLabelsInMatrix(label, job.Strategy) {
				rule.compats = maps.Clone(fixed)
				checkLabels(labels)
			}
		}
	}
	return true
}

type runnerMatrixCombination map[string]workflowlang.RawYAMLValue

func knownRunnerMatrixCombinations(strategy *workflowlang.Strategy) ([]runnerMatrixCombination, bool) {
	if strategy == nil {
		return nil, false
	}
	matrix := strategy.Matrix
	evaluated := strategy.Expression != nil
	if evaluated {
		matrix = knownStrategyMatrix(strategy.Expression)
	}
	if matrix != nil && matrix.Expression == nil {
		var complete bool
		matrix, complete = expandedMatrixInsertions(matrix)
		if !complete {
			return nil, false
		}
	}
	if matrix == nil || matrix.Expression != nil {
		return nil, false
	}
	resolved := *matrix
	var complete bool
	resolved.Include, complete = expandedMatrixCombinations(matrix.Include)
	if !complete {
		return nil, false
	}
	resolved.Exclude, complete = expandedMatrixCombinations(matrix.Exclude)
	if !complete {
		return nil, false
	}
	matrix = &resolved
	known := func(value workflowlang.RawYAMLValue) bool {
		return evaluated || !exprlang.ContainsExpression(value.String())
	}
	for _, combinations := range []*workflowlang.MatrixCombinations{matrix.Include, matrix.Exclude} {
		if combinations == nil {
			continue
		}
		if combinations.ContainsExpression() {
			return nil, false
		}
		for _, combination := range combinations.Combinations {
			for _, assign := range combination.Assigns {
				if !combination.Evaluated && !known(assign.Value) {
					return nil, false
				}
			}
		}
	}

	// GitHub permits at most 256 matrix jobs. Bound expansion before allocating rows.
	const limit = 256
	var originals []runnerMatrixCombination
	if len(matrix.Rows) != 0 {
		originals = []runnerMatrixCombination{{}}
	}
	for _, name := range slices.Sorted(maps.Keys(matrix.Rows)) {
		row := matrix.Rows[name]
		elements, complete := matrixRowElements(row, !evaluated)
		if !complete || len(elements) > 0 && len(originals) > limit/len(elements) {
			return nil, false
		}
		for _, element := range elements {
			if element.expressions && !known(element.value) {
				return nil, false
			}
		}
		expanded := make([]runnerMatrixCombination, 0, len(originals)*len(elements))
		for _, original := range originals {
			for _, element := range elements {
				combination := maps.Clone(original)
				combination[name] = element.value
				expanded = append(expanded, combination)
			}
		}
		originals = expanded
	}
	if matrix.Exclude != nil {
		originals = slices.DeleteFunc(originals, func(combination runnerMatrixCombination) bool {
			for _, exclude := range matrix.Exclude.Combinations {
				matches := true
				for name, assign := range exclude.Assigns {
					if !isYAMLValueSubset(combination[name], assign.Value, false) {
						matches = false
						break
					}
				}
				if matches {
					return true
				}
			}
			return false
		})
	}
	result := make([]runnerMatrixCombination, 0, len(originals))
	for _, original := range originals {
		result = append(result, maps.Clone(original))
	}
	if matrix.Include != nil {
		for _, include := range matrix.Include.Combinations {
			added := false
			for i, original := range originals {
				matches := true
				for name, assign := range include.Assigns {
					if value, exists := original[name]; exists && !isYAMLValueSubset(value, assign.Value, false) {
						matches = false
						break
					}
				}
				if matches {
					for name, assign := range include.Assigns {
						if _, axis := matrix.Rows[name]; !axis {
							result[i][name] = assign.Value
						}
					}
					added = true
				}
			}
			if !added {
				if len(result) == limit {
					return nil, false
				}
				combination := runnerMatrixCombination{}
				for name, assign := range include.Assigns {
					combination[name] = assign.Value
				}
				result = append(result, combination)
			}
		}
	}
	return result, true
}

func runnerMatrixValueLabels(value workflowlang.RawYAMLValue) []*workflowlang.String {
	if object, ok := value.(*workflowlang.RawYAMLObject); ok {
		value = object.Props["labels"]
	}
	var labels []*workflowlang.String
	appendLabel := func(value workflowlang.RawYAMLValue) {
		if scalar, ok := value.(*workflowlang.RawYAMLString); ok {
			labels = append(labels, runnerExpressionLabel(scalar.ScalarValue(), scalar.Pos()))
		}
	}
	if array, ok := value.(*workflowlang.RawYAMLArray); ok {
		for _, item := range array.Elems {
			appendLabel(item)
		}
	} else {
		appendLabel(value)
	}
	return labels
}

func (rule *RuleRunnerLabel) checkConflict(comp runnerOSCompat, label *workflowlang.String) bool {
	for c, l := range rule.compats {
		if c&comp == 0 {
			rule.Errorf(label.Pos, "label %q conflicts with label %q defined at %s. note: to run your job on each workers, use matrix", label.Value, l.Value, l.Pos)
			return false
		}
	}
	return true
}

func (rule *RuleRunnerLabel) checkCompat(comp runnerOSCompat, label *workflowlang.String) {
	if comp == compatInvalid || !rule.checkConflict(comp, label) {
		return
	}
	if _, ok := rule.compats[comp]; !ok {
		rule.compats[comp] = label
	}
}

func (rule *RuleRunnerLabel) checkMatrixLabels(groups [][]*workflowlang.String) {
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
