package runner

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/stringutil"
	matrixmodel "actionlint.kjanat.dev/v2/internal/workflow/matrix"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
	"actionlint.kjanat.dev/v2/runner"
)

type runnerOSCompat uint

const (
	compatInvalid    runnerOSCompat = 0
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

var allGitHubHostedRunnerLabels = []string{
	"windows-latest", "windows-latest-8-cores", "windows-2025", "windows-2025-vs2026", "windows-2022", "windows-11-arm", "windows-11-vs2026-arm",
	"ubuntu-slim", "ubuntu-latest", "ubuntu-latest-4-cores", "ubuntu-latest-8-cores", "ubuntu-latest-16-cores", "ubuntu-26.04", "ubuntu-26.04-arm", "ubuntu-24.04", "ubuntu-24.04-arm", "ubuntu-22.04", "ubuntu-22.04-arm",
	"xcode-27", "xcode-27-xlarge", "macos-latest", "macos-latest-xlarge", "macos-latest-large", "macos-26-intel", "macos-26-xlarge", "macos-26-large", "macos-26", "macos-15-intel", "macos-15-xlarge", "macos-15-large", "macos-15", "macos-14-xlarge", "macos-14-large", "macos-14",
}

var selfHostedRunnerPresetOSLabels = []string{"linux", "macos", "windows"}
var selfHostedRunnerPresetOtherLabels = []string{"self-hosted", "x64", "arm", "arm64"}

var defaultRunnerOSCompats = map[string]runnerOSCompat{
	"ubuntu-slim":            compatUbuntu2404,
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
	"windows-11-arm":         compatWindows11Arm | compatWindows11VS2026Arm,
	"windows-11-vs2026-arm":  compatWindows11VS2026Arm,
	"linux":                  compatUbuntu2604 | compatUbuntu2404 | compatUbuntu2204 | compatUbuntu2604ARM | compatUbuntu2404ARM | compatUbuntu2204ARM,
	"macos":                  compatXcode270 | compatXcode270XL | compatMacOS260 | compatMacOS260Intel | compatMacOS260L | compatMacOS260XL | compatMacOS150 | compatMacOS150Intel | compatMacOS150L | compatMacOS150XL | compatMacOS140 | compatMacOS140L | compatMacOS140XL,
	"windows":                compatWindows2025VS2026 | compatWindows2022 | compatWindows11Arm | compatWindows11VS2026Arm,
}

type labelFact struct {
	text   string
	source string
	start  diagnostic.Position
}

type checker struct {
	rejections []runner.Rejection
	labels     []string
	compats    map[runnerOSCompat]labelFact
}

func Check(job documentparse.JobView, customLabels []string) []diagnostic.Diagnostic {
	if !job.Present() {
		return nil
	}
	selection := job.RunsOn().Selection()
	strategy := job.Strategy().MatrixResolution()
	check := checker{labels: customLabels}
	combinations, complete := strategy.DeclarationCombinations(256)
	if !complete {
		combinations = []matrixmodel.Combination{{}}
		for _, item := range selectionValues(selection) {
			property := runnerMatrixProperty(item.Source())
			if property == "" {
				continue
			}
			for _, candidate := range strategy.DeclarationReachable(property).Values {
				combinations = append(combinations, (matrixmodel.Combination{}).With(property, candidate))
			}
		}
	}
	for _, combination := range combinations {
		check.checkSelection(selection, combination)
	}
	findings := make([]diagnostic.Diagnostic, len(check.rejections))
	seen := map[string]bool{}
	filtered := findings[:0]
	for _, rejection := range check.rejections {
		finding := diagnostic.Diagnostic{Rule: "runner-label", Start: rejection.Position, Message: rejection.Reason}
		key := fmt.Sprintf("%d:%d:%s", finding.Start.Line, finding.Start.Column, finding.Message)
		if strings.Contains(finding.Message, " conflicts with label ") {
			key = fmt.Sprintf("%d:%d:conflict", finding.Start.Line, finding.Start.Column)
		}
		if !seen[key] {
			seen[key] = true
			filtered = append(filtered, finding)
		}
	}
	return filtered
}

func (rule *checker) checkSelection(selection value.Value, combination matrixmodel.Combination) {
	labels := selectionLabels(selection, combination)
	allowNull := hasMatrixReference(selection)
	if len(labels) == 0 {
		return
	}
	rule.compats = map[runnerOSCompat]labelFact{}
	for _, label := range labels {
		if label.Kind() == value.Null && !allowNull {
			continue
		}
		fact, ok := knownLabel(label)
		if !ok {
			continue
		}
		if len(labels) == 1 {
			rule.verifyRunnerLabel(fact)
		} else {
			rule.checkCompat(rule.verifyRunnerLabel(fact), fact)
		}
	}
}

func hasMatrixReference(selection value.Value) bool {
	for _, candidate := range selectionValues(selection) {
		if runnerMatrixProperty(candidate.Source()) != "" {
			return true
		}
	}
	return false
}

func (rule *checker) verifyRunnerLabel(label labelFact) runnerOSCompat {
	lower := strings.ToLower(label.text)
	if compat, ok := defaultRunnerOSCompats[lower]; ok {
		return compat
	}
	for _, preset := range selfHostedRunnerPresetOtherLabels {
		if strings.EqualFold(label.text, preset) {
			return compatInvalid
		}
	}
	for _, known := range rule.labels {
		matched, err := path.Match(known, label.text)
		if err != nil {
			rule.Errorf(label.start, "label pattern %q is an invalid glob. kindly check list of labels in actionlint.yaml config file: %v", known, err)
			return compatInvalid
		}
		if matched {
			if windowsLabel(label.text) {
				return defaultRunnerOSCompats["windows"]
			}
			return compatInvalid
		}
	}
	rule.Errorf(label.start, "label %q is unknown. available labels are %s. if it is a custom label for self-hosted runner, set list of labels in actionlint.yaml config file", label.text, stringutil.QuotesAll(allGitHubHostedRunnerLabels, selfHostedRunnerPresetOtherLabels, selfHostedRunnerPresetOSLabels, rule.labels))
	return compatInvalid
}

func (rule *checker) checkCompat(compat runnerOSCompat, label labelFact) {
	if compat == compatInvalid {
		return
	}
	for previousCompat, previous := range rule.compats {
		if previousCompat&compat == 0 {
			rule.Errorf(label.start, "label %q conflicts with label %q defined at line:%d,col:%d. note: to run your job on each workers, use matrix", label.text, previous.text, previous.start.Line, previous.start.Column)
			return
		}
	}
	if _, exists := rule.compats[compat]; !exists {
		rule.compats[compat] = label
	}
}

func (rule *checker) Errorf(position diagnostic.Position, format string, args ...any) {
	rule.rejections = append(rule.rejections, runner.Rejection{Position: position, Reason: fmt.Sprintf(format, args...)})
}

func selectionLabels(selection value.Value, combination matrixmodel.Combination) []value.Value {
	candidate := selection
	if selection.State() == value.Dynamic {
		if resolved, ok := resolveMatrixValue(selection, combination); ok {
			return labelItems(resolved, true)
		}
		return nil
	}
	if selection.Known() && selection.Kind() == value.Object {
		labels, ok := selection.Prop("labels")
		if !ok {
			return nil
		}
		candidate = labels
	}
	if candidate.Known() && candidate.Kind() == value.Array {
		var result []value.Value
		for item := range candidate.Items() {
			result = append(result, expressionLabels(item, combination)...)
		}
		return result
	}
	return expressionLabels(candidate, combination)
}

func expressionLabels(expression value.Value, combination matrixmodel.Combination) []value.Value {
	if expression.State() == value.Missing {
		return nil
	}
	if expression.State() == value.Dynamic {
		if property := runnerMatrixProperty(expression.Source()); property != "" {
			if item, ok := combination.Get(property); ok {
				return labelItems(item, true)
			}
		}
		return nil
	}
	return labelItems(expression, false)
}

func labelItems(candidate value.Value, nullIsLabel bool) []value.Value {
	if candidate.Known() && candidate.Kind() == value.Object {
		candidate, _ = candidate.Prop("labels")
		if candidate.State() == value.Missing {
			return nil
		}
	}
	if candidate.Known() && candidate.Kind() == value.Array {
		var labels []value.Value
		for item := range candidate.Items() {
			if item.Kind() == value.Null && !nullIsLabel {
				continue
			}
			labels = append(labels, item)
		}
		return labels
	}
	if candidate.Kind() == value.Null && !nullIsLabel {
		return nil
	}
	return []value.Value{candidate}
}

func knownLabel(candidate value.Value) (labelFact, bool) {
	text, ok := candidate.ScalarString()
	if !ok {
		return labelFact{}, false
	}
	return labelFact{text: text, source: candidate.Source(), start: candidate.Origin().Span.Start}, true
}

func runnerMatrixProperty(expression string) string {
	var receiver exprlang.Node
	var property string
	switch reference := exprlang.ParseAssignedExpression(strings.TrimSpace(expression)).(type) {
	case *exprlang.ObjectDerefNode:
		receiver, property = reference.Receiver, reference.Property
	case *exprlang.IndexAccessNode:
		key, literal := reference.Index.(*exprlang.StringNode)
		if !literal {
			return ""
		}
		receiver, property = reference.Operand, key.Value
	}
	if context, variable := receiver.(*exprlang.VariableNode); variable && strings.EqualFold(context.Name, "matrix") {
		return strings.ToLower(property)
	}
	return ""
}

func PlatformOf(selection value.Value) Platform {
	platform := Any
	for _, label := range Labels(selection) {
		fact, ok := knownLabel(label)
		if !ok {
			continue
		}
		name := strings.ToLower(fact.text)
		current := Any
		if windowsLabel(name) {
			current = Windows
		} else if defaultRunnerOSCompats[name]&(defaultRunnerOSCompats["macos"]|defaultRunnerOSCompats["linux"]) != 0 {
			current = MacOrLinux
		}
		if current != Any && platform != Any && platform != current {
			return Any
		}
		if current != Any {
			platform = current
		}
	}
	return platform
}

func Labels(selection value.Value) []value.Value {
	return selectionLabels(selection, matrixmodel.Combination{})
}

func Hosted(selection value.Value) bool {
	if selection.Known() && selection.Kind() == value.Object {
		if group, ok := selection.Prop("group"); ok && group.State() != value.Missing {
			return false
		}
	}
	labels := Labels(selection)
	if len(labels) != 1 {
		return false
	}
	label, ok := knownLabel(labels[0])
	return ok && slices.Contains(allGitHubHostedRunnerLabels, strings.ToLower(label.text))
}

func windowsLabel(label string) bool {
	return slices.Contains(strings.Split(strings.ToLower(label), "-"), "windows")
}
