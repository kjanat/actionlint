package actionlint

import (
	"math"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

// RuleMatrix is a rule checker to check 'matrix' field of job.
type RuleMatrix struct {
	RuleBase
}

// NewRuleMatrix creates new RuleMatrix instance.
func NewRuleMatrix() *RuleMatrix {
	return &RuleMatrix{
		RuleBase: RuleBase{
			name: "matrix",
			desc: "Checks for matrix combinations in \"matrix:\"",
		},
	}
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleMatrix) VisitJobPre(n *Job) error {
	if n.Strategy == nil {
		return nil
	}

	m := n.Strategy.Matrix
	evaluated := n.Strategy.Expression != nil
	if evaluated {
		m = knownStrategyMatrix(n.Strategy.Expression)
	}
	if m == nil || m.Expression != nil {
		return nil
	}

	for _, row := range m.Rows {
		rule.checkDuplicateInRow(row)
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
	return nil
}

func knownStrategyMatrix(expression *String) *Matrix {
	value, known := workflowExpressionLiteral(expression)
	if !known {
		return nil
	}
	strategy, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	matrix, ok := workflowObjectProperty(strategy, "matrix").(map[string]any)
	if !ok || len(workflowExpressionLiteralErrors(workflowStrategy.props["matrix"], matrix, "matrix")) != 0 {
		return nil
	}
	var node yaml.Node
	if err := node.Encode(matrix); err != nil {
		return nil
	}
	var setPosition func(*yaml.Node)
	setPosition = func(n *yaml.Node) {
		n.Line, n.Column = expression.Pos.Line, expression.Pos.Col
		for _, child := range n.Content {
			setPosition(child)
		}
	}
	setPosition(&node)
	return (&parser{}).parseMatrix(expression.Pos, &node)
}

func (rule *RuleMatrix) checkDuplicateInRow(row *MatrixRow) {
	if row.Values == nil {
		return // Give up when ${{ }} is specified
	}
	seen := make([]RawYAMLValue, 0, len(row.Values))
	for _, v := range row.Values {
		ok := true
		for _, p := range seen {
			if p.Equals(v) {
				rule.Errorf(
					v.Pos(),
					"duplicate value %s is found in matrix %q. the same value is at %s",
					v.String(),
					row.Name.Value,
					p.Pos().String(),
				)
				ok = false
				break
			}
		}
		if ok {
			seen = append(seen, v)
		}
	}
}

// Matrix filters compare selected properties and indices with expression equality.
func isYAMLValueSubset(value, filter RawYAMLValue, expressions bool) bool {
	// Dynamic values or filter leaves cannot establish a mismatch. Evaluated
	// expression results use expressions=false so embedded syntax remains data.
	if expressions {
		for _, item := range []RawYAMLValue{value, filter} {
			if scalar, ok := item.(*RawYAMLString); ok && ContainsExpression(scalar.Value) {
				return true
			}
		}
	}
	lookup := func(key string) RawYAMLValue {
		switch value := value.(type) {
		case *RawYAMLObject:
			return value.Props[key]
		case *RawYAMLArray:
			index := matrixFilterNumber(key)
			if index >= 0 && index < float64(len(value.Elems)) && index <= math.MaxInt32 {
				return value.Elems[int(index)]
			}
		}
		return nil
	}
	switch filter := filter.(type) {
	case *RawYAMLObject:
		for key, item := range filter.Props {
			if !isYAMLValueSubset(lookup(key), item, expressions) {
				return false
			}
		}
		return true
	case *RawYAMLArray:
		for i, item := range filter.Elems {
			if !isYAMLValueSubset(lookup(strconv.Itoa(i)), item, expressions) {
				return false
			}
		}
		return true
	case *RawYAMLString:
		var actual any
		if scalar, ok := value.(*RawYAMLString); ok {
			actual = scalar.scalarValue()
		} else if value != nil {
			return false
		}
		expected := filter.scalarValue()
		if a, ok := actual.(string); ok {
			if b, ok := expected.(string); ok {
				return strings.EqualFold(a, b)
			}
		}
		return matrixFilterNumber(actual) == matrixFilterNumber(expected)
	default:
		return false
	}
}

func matrixFilterNumber(value any) float64 {
	switch value := value.(type) {
	case nil:
		return 0
	case bool:
		if value {
			return 1
		}
		return 0
	case float64:
		return value
	case string:
		value = strings.TrimSpace(value)
		switch strings.ToLower(value) {
		case "":
			return 0
		case "infinity", "+infinity":
			return math.Inf(1)
		case "-infinity":
			return math.Inf(-1)
		}
		if reCoreSchemaFloat.MatchString(value) {
			if number, err := strconv.ParseFloat(value, 64); err == nil || math.IsInf(number, 0) {
				return number
			}
		} else if (strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0o")) && isCoreSchemaInt(value) {
			// ExpressionUtility.ParseNumber uses signed 32-bit hexadecimal/octal conversion.
			if number, err := strconv.ParseUint(value, 0, 32); err == nil {
				if number > math.MaxInt32 {
					return float64(number) - (1 << 32)
				}
				return float64(number)
			}
		}
	}
	return math.NaN()
}

func (rule *RuleMatrix) checkExclude(m *Matrix, expressions bool) {
	if m.Exclude == nil || len(m.Exclude.Combinations) == 0 {
		return
	}

	if len(m.Rows) == 0 {
		rule.Error(m.Pos, "\"exclude\" section exists but no matrix variation exists")
		return
	}

	rows := make(map[string][]RawYAMLValue, len(m.Rows))
	ignored := map[string]struct{}{}

	for n, r := range m.Rows {
		if r.Expression != nil {
			ignored[n] = struct{}{}
			continue
		}
		rows[n] = r.Values
	}

	for _, c := range m.Exclude.Combinations {
	Exclude:
		for k, a := range c.Assigns {
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
					sortedQuotes(ss),
				)
				continue
			}

			for _, v := range row {
				if isYAMLValueSubset(v, a.Value, expressions) {
					continue Exclude
				}
			}

			ss := make([]string, 0, len(row))
			for _, v := range row {
				ss = append(ss, v.String())
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
