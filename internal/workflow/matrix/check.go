package matrix

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/diagnostic"
	exprlang "actionlint.kjanat.dev/internal/expression"
	"actionlint.kjanat.dev/internal/stringutil"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/schema"
	"actionlint.kjanat.dev/internal/yaml"
)

// Check validates one job's matrix and optionally checks loose filter types.
// It accepts workflow declarations and returns diagnostics without a visitor host.
func Check(job *workflowlang.Job, mixedTypes bool) []diagnostic.Diagnostic {
	checker := &checker{mixedTypes: mixedTypes}
	checker.checkJob(job)
	return checker.findings
}

type checker struct {
	findings   []diagnostic.Diagnostic
	mixedTypes bool
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *checker) checkJob(n *workflowlang.Job) {
	if n.Strategy == nil {
		return
	}

	m := n.Strategy.Matrix
	evaluated := n.Strategy.Expression != nil
	if evaluated {
		m = KnownStrategy(n.Strategy.Expression)
	}
	if m != nil && m.Expression != nil {
		value, known := workflowlang.WorkflowExpressionLiteral(m.Expression)
		if !known {
			return
		}
		m = knownLiteral(value, m.Expression.Pos)
		evaluated = true
	}
	if m == nil {
		return
	}

	complete := true
	if m.Expression == nil {
		m, complete = Expand(m)
		if m == nil {
			return
		}
	}
	for name, row := range m.Rows {
		if row.Name != nil {
			name = row.Name.Value
		}
		rule.checkDuplicateInRow(name, row, !evaluated)
	}
	if !complete {
		return
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
	if rule.mixedTypes {
		rule.checkFilterTypes(m, !evaluated)
	}
}

// KnownStrategy projects a constant strategy expression into matrix declarations.
func KnownStrategy(expression *workflowlang.String) *workflowlang.Matrix {
	value, known := workflowlang.WorkflowExpressionLiteral(expression)
	if !known {
		return nil
	}
	strategy, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return knownLiteral(schema.Property(strategy, "matrix"), expression.Pos)
}

func knownLiteral(value any, position *workflowlang.Pos) *workflowlang.Matrix {
	matrix, ok := value.(map[string]any)
	if !ok || len(schema.LiteralErrors(schema.Matrix, matrix, "matrix")) != 0 {
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

func (rule *checker) checkDuplicateInRow(name string, row *workflowlang.MatrixRow, expressions bool) {
	elements, _ := RowElements(row, expressions)
	seen := make([]Element, 0, len(elements))
	for _, element := range elements {
		v := element.Value
		dynamic := element.Expressions && matrixValueContainsExpression(v)
		ok := true
		for _, p := range seen {
			if dynamic != (p.Expressions && matrixValueContainsExpression(p.Value)) {
				continue
			}
			if p.Value.Equals(v) {
				rule.Errorf(
					v.Pos(),
					"duplicate value %s is found in matrix %q. the same value is at %s",
					v.String(),
					name,
					p.Value.Pos().String(),
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

// Subset compares selected properties and indices with expression equality.
func Subset(value, filter workflowlang.RawYAMLValue, expressions bool) bool {
	return isYAMLValueSubsetWithExpressions(value, filter, expressions, expressions)
}

func isYAMLValueSubsetWithExpressions(value, filter workflowlang.RawYAMLValue, valueExpressions, filterExpressions bool) bool {
	// Dynamic values or filter leaves cannot establish a mismatch. Evaluated
	// expression results use expressions=false so embedded syntax remains data.
	for _, item := range []Element{{value, valueExpressions}, {filter, filterExpressions}} {
		if scalar, ok := item.Value.(*workflowlang.RawYAMLString); item.Expressions && ok && exprlang.ContainsExpression(scalar.Value) {
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
			if !isYAMLValueSubsetWithExpressions(actual, item.Value, actualExpressions, item.Expressions) {
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

func (rule *checker) checkExclude(m *workflowlang.Matrix, expressions bool) {
	combinations := Combinations(m.Exclude, expressions)
	if len(combinations) == 0 {
		return
	}

	if len(m.Rows) == 0 {
		rule.Error(m.Pos, "\"exclude\" section exists but no matrix variation exists")
		return
	}

	rows := make(map[string][]Element, len(m.Rows))
	ignored := map[string]struct{}{}

	for n, r := range m.Rows {
		values, complete := RowElements(r, expressions)
		if !complete {
			ignored[n] = struct{}{}
		}
		rows[n] = values
	}

	for _, c := range combinations {
	Exclude:
		for k, a := range c.Combination.Assigns {
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
				if isYAMLValueSubsetWithExpressions(v.Value, a.Value, v.Expressions, c.Expressions) {
					continue Exclude
				}
			}

			ss := make([]string, 0, len(row))
			for _, v := range row {
				ss = append(ss, v.Value.String())
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

func (rule *checker) Error(pos *workflowlang.Pos, message string) {
	rule.findings = append(rule.findings, findingAt(pos, "matrix", "%s", message))
}
func (rule *checker) Errorf(pos *workflowlang.Pos, format string, args ...any) {
	rule.findings = append(rule.findings, findingAt(pos, "matrix", format, args...))
}
func findingAt(pos *workflowlang.Pos, rule, format string, args ...any) diagnostic.Diagnostic {
	return diagnostic.Diagnostic{Start: diagnostic.Position{Line: pos.Line, Column: pos.Col}, Rule: rule, Message: fmt.Sprintf(format, args...)}
}
