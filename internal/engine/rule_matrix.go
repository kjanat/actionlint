package engine

import (
	"math"
	"strconv"
	"strings"

	exprlang "actionlint.kjanat.dev/internal/expression"
	stringutil "actionlint.kjanat.dev/internal/stringutil"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"go.yaml.in/yaml/v4"
)

// RuleMatrix is a rule checker to check 'matrix' field of job.
type RuleMatrix struct {
	RuleBase
}

// NewRuleMatrix creates new RuleMatrix instance.
func NewRuleMatrix() *RuleMatrix {
	return &RuleMatrix{
		RuleBase: builtinRuleBase("matrix"),
	}
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleMatrix) VisitJobPre(n *workflowlang.Job) error {
	if n.Strategy == nil {
		return nil
	}

	m := n.Strategy.Matrix
	evaluated := n.Strategy.Expression != nil
	if evaluated {
		m = knownStrategyMatrix(n.Strategy.Expression)
	}
	if m != nil && m.Expression != nil {
		value, known := workflowlang.WorkflowExpressionLiteral(m.Expression)
		if !known {
			return nil
		}
		m = knownLiteralMatrix(value, m.Expression.Pos)
		evaluated = true
	}
	if m == nil {
		return nil
	}

	complete := true
	if m.Expression == nil {
		m, complete = expandedMatrixInsertions(m)
		if m == nil {
			return nil
		}
	}
	for name, row := range m.Rows {
		if row.Name != nil {
			name = row.Name.Value
		}
		rule.checkDuplicateInRow(name, row, !evaluated)
	}
	if !complete {
		return nil
	}

	// Note:
	// Any new value can be set in the section as new combination. It can add new value to existing
	// column also.
	//
	// matrix:
	//   os: [ubuntu-latest, macos-latest]
	//   include:
	//     - os: windows-latest
	//       sh: pwsh

	rule.checkExclude(m, !evaluated)
	if diagnosticLevel(rule.config, "mixed-type-matrix-filters") != "off" {
		rule.checkFilterTypes(m, !evaluated)
	}
	return nil
}

func knownStrategyMatrix(expression *workflowlang.String) *workflowlang.Matrix {
	value, known := workflowlang.WorkflowExpressionLiteral(expression)
	if !known {
		return nil
	}
	strategy, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return knownLiteralMatrix(workflowObjectProperty(strategy, "matrix"), expression.Pos)
}

func knownLiteralMatrix(value any, position *workflowlang.Pos) *workflowlang.Matrix {
	matrix, ok := value.(map[string]any)
	if !ok || len(workflowExpressionLiteralErrors(workflowStrategy.props["matrix"], matrix, "matrix")) != 0 {
		return nil
	}
	var node yaml.Node
	if err := node.Encode(matrix); err != nil {
		return nil
	}
	var setPosition func(*yaml.Node)
	setPosition = func(n *yaml.Node) {
		n.Line, n.Column = position.Line, position.Col
		for _, child := range n.Content {
			setPosition(child)
		}
	}
	setPosition(&node)
	return (&workflowlang.Parser{}).ParseMatrixMapping(position, &node, true, true)
}

func (rule *RuleMatrix) checkDuplicateInRow(name string, row *workflowlang.MatrixRow, expressions bool) {
	elements, _ := matrixRowElements(row, expressions)
	seen := make([]matrixFilterElement, 0, len(elements))
	for _, element := range elements {
		v := element.value
		dynamic := element.expressions && matrixValueContainsExpression(v)
		ok := true
		for _, p := range seen {
			if dynamic != (p.expressions && matrixValueContainsExpression(p.value)) {
				continue
			}
			if p.value.Equals(v) {
				rule.Errorf(
					v.Pos(),
					"duplicate value %s is found in matrix %q. the same value is at %s",
					v.String(),
					name,
					p.value.Pos().String(),
				)
				ok = false
				break
			}
		}
		if ok {
			seen = append(seen, element)
		}
	}
}

// Matrix filters compare selected properties and indices with expression equality.
func isYAMLValueSubset(value, filter workflowlang.RawYAMLValue, expressions bool) bool {
	return isYAMLValueSubsetWithExpressions(value, filter, expressions, expressions)
}

func isYAMLValueSubsetWithExpressions(value, filter workflowlang.RawYAMLValue, valueExpressions, filterExpressions bool) bool {
	// Dynamic values or filter leaves cannot establish a mismatch. Evaluated
	// expression results use expressions=false so embedded syntax remains data.
	for _, item := range []matrixFilterElement{{value, valueExpressions}, {filter, filterExpressions}} {
		if scalar, ok := item.value.(*workflowlang.RawYAMLString); item.expressions && ok && exprlang.ContainsExpression(scalar.Value) {
			return true
		}
	}
	switch filter := filter.(type) {
	case *workflowlang.RawYAMLObject:
		lookup := newMatrixFilterLookup(value, valueExpressions)
		for key, item := range filter.Props {
			actual, actualExpressions, uncertain := lookup.at(key)
			if uncertain {
				continue
			}
			if !isYAMLValueSubsetWithExpressions(actual, item, actualExpressions, filterExpressions) {
				return false
			}
		}
		return true
	case *workflowlang.RawYAMLArray:
		filters, _ := matrixFilterElements(filter, filterExpressions)
		lookup := newMatrixFilterLookup(value, valueExpressions)
		for i, item := range filters {
			actual, actualExpressions, uncertain := lookup.at(strconv.Itoa(i))
			if uncertain {
				return true
			}
			if !isYAMLValueSubsetWithExpressions(actual, item.value, actualExpressions, item.expressions) {
				return false
			}
		}
		return true
	case *workflowlang.RawYAMLString:
		var actual any
		if scalar, ok := value.(*workflowlang.RawYAMLString); ok {
			actual = scalar.ScalarValue()
		} else if value != nil {
			return false
		}
		expected := filter.ScalarValue()
		if a, ok := actual.(string); ok {
			if b, ok := expected.(string); ok {
				return strings.EqualFold(a, b)
			}
		}
		return exprlang.ToNumber(actual) == exprlang.ToNumber(expected)
	default:
		return false
	}
}

func matrixFilterValueAt(value workflowlang.RawYAMLValue, key string) workflowlang.RawYAMLValue {
	switch value := value.(type) {
	case *workflowlang.RawYAMLObject:
		return value.Props[key]
	case *workflowlang.RawYAMLArray:
		index := exprlang.ToNumber(key)
		if index >= 0 && index < float64(len(value.Elems)) && index <= math.MaxInt32 {
			return value.Elems[int(index)]
		}
	}
	return nil
}

func (rule *RuleMatrix) checkExclude(m *workflowlang.Matrix, expressions bool) {
	combinations := matrixExcludeCombinations(m.Exclude, expressions)
	if len(combinations) == 0 {
		return
	}

	if len(m.Rows) == 0 {
		rule.Error(m.Pos, "\"exclude\" section exists but no matrix variation exists")
		return
	}

	rows := make(map[string][]matrixFilterElement, len(m.Rows))
	ignored := map[string]struct{}{}

	for n, r := range m.Rows {
		values, complete := matrixRowElements(r, expressions)
		if !complete {
			ignored[n] = struct{}{}
		}
		rows[n] = values
	}

	for _, c := range combinations {
	Exclude:
		for k, a := range c.combination.Assigns {
			if _, ok := ignored[k]; ok {
				continue
			}
			row, ok := rows[k]
			if !ok {
				ss := make([]string, 0, len(rows))
				for k := range rows {
					ss = append(ss, k)
				}
				rule.Errorf(
					a.Key.Pos,
					"%q in \"exclude\" section does not exist in matrix. available matrix configurations are %s",
					k,
					stringutil.SortedQuotes(ss),
				)
				continue
			}

			for _, v := range row {
				if isYAMLValueSubsetWithExpressions(v.value, a.Value, v.expressions, c.expressions) {
					continue Exclude
				}
			}

			ss := make([]string, 0, len(row))
			for _, v := range row {
				ss = append(ss, v.value.String())
			}
			rule.Errorf(
				a.Value.Pos(),
				"value %s in \"exclude\" does not match in matrix %q combinations. possible values are %s",
				a.Value.String(),
				k,
				strings.Join(ss, ", "), // Note: do not use quotesBuilder
			)
		}
	}
}
