package matrix

import (
	"iter"
	"maps"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/diagnostic"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

type Axis struct {
	Name     string
	Values   []value.Value
	Complete bool
	Origin   value.Origin
}

type Combination struct{ values map[string]value.Value }

func (c Combination) Get(name string) (value.Value, bool) {
	v, ok := c.values[strings.ToLower(name)]
	return v, ok
}

// Values yields combination fields in stable key order.
func (c Combination) Values() iter.Seq2[string, value.Value] {
	values := maps.Clone(c.values)
	keys := slices.Sorted(maps.Keys(values))
	return func(yield func(string, value.Value) bool) {
		for _, name := range keys {
			if !yield(name, values[name]) {
				return
			}
		}
	}
}

func (c Combination) With(name string, item value.Value) Combination {
	values := maps.Clone(c.values)
	if values == nil {
		values = map[string]value.Value{}
	}
	values[strings.ToLower(name)] = item
	return Combination{values: values}
}

type Reach struct {
	Values       []value.Value
	MayBeMissing bool
	Complete     bool
}

type Resolution struct {
	state       value.State
	axes        []Axis
	include     []Combination
	exclude     []Combination
	filterKnown bool
	unknownKeys bool
	origin      value.Origin
}

func (r Resolution) State() value.State { return r.state }

func (r Resolution) Open() bool {
	return r.unknownKeys || r.state == value.Dynamic || r.state == value.Invalid
}

func (r Resolution) Axes() iter.Seq[Axis] {
	axes := slices.Clone(r.axes)
	return func(yield func(Axis) bool) {
		for _, axis := range axes {
			axis.Values = slices.Clone(axis.Values)
			if !yield(axis) {
				return
			}
		}
	}
}

// ReachableNames yields base-axis and include-only names in stable order.
func (r Resolution) ReachableNames() iter.Seq[string] {
	names := make([]string, 0, len(r.axes))
	seen := make(map[string]struct{}, len(r.axes))
	for _, axis := range r.axes {
		names = append(names, axis.Name)
		seen[axis.Name] = struct{}{}
	}
	for _, inclusion := range r.include {
		for name := range inclusion.values {
			if _, exists := seen[name]; exists {
				continue
			}
			names = append(names, name)
			seen[name] = struct{}{}
		}
	}
	slices.Sort(names)
	return slices.Values(names)
}

func Resolve(strategy *workflowlang.Strategy) Resolution {
	if strategy == nil {
		return Resolution{state: value.Missing, filterKnown: true}
	}
	if strategy.Expression != nil {
		semantic := expression(strategy.Expression)
		matrix, ok := semantic.Prop("matrix")
		if !semantic.Known() || !ok || matrix.State() == value.Dynamic || matrix.State() == value.Invalid {
			return Resolution{state: semantic.State(), filterKnown: false, origin: semantic.Origin()}
		}
		return resolveSemanticMatrix(matrix)
	}
	if strategy.Matrix == nil {
		return Resolution{state: value.Missing, filterKnown: true}
	}
	return resolveDeclaration(strategy.Matrix)
}

func resolveDeclaration(matrix *workflowlang.Matrix) Resolution {
	if matrix.Expression != nil {
		return resolveSemanticMatrix(expression(matrix.Expression))
	}
	r := Resolution{state: value.Literal, filterKnown: true, origin: origin(matrix.Pos, false)}
	for _, name := range slices.Sorted(maps.Keys(matrix.Rows)) {
		row := matrix.Rows[name]
		key := value.YAMLScalar(name, workflowlang.YamlTagStr, value.Origin{})
		if row.Name != nil {
			key = workflowlang.SemanticValue(row.Name)
			if row.Evaluated {
				key = value.YAMLScalar(row.Name.Value, workflowlang.YamlTagStr, key.Origin())
			}
		}
		axisName, keyState := mapping.ResolveKey(key)
		if keyState != mapping.KnownKey {
			r.unknownKeys = true
			continue
		}
		axisName = strings.ToLower(axisName)
		axis := Axis{Name: axisName, Complete: true, Origin: origin(rowPosition(row), false)}
		if row.Expression != nil {
			v := expression(row.Expression)
			if v.Known() && v.Kind() == value.Array {
				for item := range v.Items() {
					axis.Values = append(axis.Values, item)
				}
			} else {
				axis.Complete = false
				axis.Values = append(axis.Values, v)
			}
		} else {
			for _, raw := range row.Values {
				scalar, isScalar := raw.(*workflowlang.RawYAMLString)
				isExpression := isScalar && !row.Evaluated && strings.Contains(scalar.Value, "${{")
				v := rawValue(raw, !row.Evaluated)
				if dynamic(v) {
					axis.Complete = false
				}
				if isExpression && v.Known() && v.Kind() == value.Array {
					for item := range v.Items() {
						axis.Values = append(axis.Values, item)
					}
				} else {
					axis.Values = append(axis.Values, v)
				}
			}
		}
		r.axes = append(r.axes, axis)
	}
	for _, inserted := range matrix.Insertions {
		part := resolveDeclaration(inserted)
		if part.state == value.Dynamic || part.state == value.Invalid {
			r.state = part.state
		}
		for _, axis := range part.axes {
			if axisIndex(r.axes, axis.Name) >= 0 {
				r.state, r.filterKnown = value.Invalid, false
				continue
			}
			r.axes = append(r.axes, axis)
		}
		r.include = append(r.include, part.include...)
		r.exclude = append(r.exclude, part.exclude...)
		r.filterKnown = r.filterKnown && part.filterKnown
		r.unknownKeys = r.unknownKeys || part.unknownKeys
	}
	if matrix.Include != nil {
		r.include, r.filterKnown = declarationCombinations(matrix.Include, r.filterKnown)
	}
	if matrix.Exclude != nil {
		var complete bool
		r.exclude, complete = declarationCombinations(matrix.Exclude, true)
		r.filterKnown = r.filterKnown && complete
	}
	return r
}

func resolveSemanticMatrix(matrix value.Value) Resolution {
	r := Resolution{state: matrix.State(), filterKnown: matrix.Known(), origin: matrix.Origin()}
	if !matrix.Known() || matrix.Kind() != value.Object {
		r.state, r.filterKnown = value.Invalid, false
		return r
	}
	for name, row := range matrix.Props() {
		if strings.EqualFold(name, "include") || strings.EqualFold(name, "exclude") {
			filters, ok := semanticCombinations(row, false)
			if !ok {
				r.filterKnown = false
			} else if strings.EqualFold(name, "include") {
				r.include = filters
			} else {
				r.exclude = filters
			}
			continue
		}
		axis := Axis{Name: strings.ToLower(name), Complete: row.Known(), Origin: row.Origin()}
		if row.Known() && row.Kind() == value.Array {
			for item := range row.Items() {
				axis.Values = append(axis.Values, item)
			}
		} else {
			axis.Complete = false
		}
		r.axes = append(r.axes, axis)
	}
	slices.SortFunc(r.axes, func(a, b Axis) int { return strings.Compare(a.Name, b.Name) })
	return r
}

func (r Resolution) Combinations(limit int) ([]Combination, bool) {
	if r.state == value.Missing {
		return []Combination{{values: map[string]value.Value{}}}, true
	}
	if r.state == value.Dynamic || r.state == value.Invalid || r.unknownKeys || !r.filterKnown || limit < 0 {
		return nil, false
	}
	combinations := []Combination{{values: map[string]value.Value{}}}
	if len(r.axes) == 0 {
		combinations = nil
	}
	for _, axis := range r.axes {
		if !axis.Complete || len(axis.Values) != 0 && len(combinations) > limit/len(axis.Values) {
			return nil, false
		}
		var next []Combination
		for _, combination := range combinations {
			for _, item := range axis.Values {
				values := maps.Clone(combination.values)
				values[axis.Name] = item
				next = append(next, Combination{values: values})
			}
		}
		combinations = next
	}
	if r.state != value.Missing && len(r.axes) == 0 && len(r.include) != 0 {
		combinations = []Combination{{values: map[string]value.Value{}}}
	}
	combinations = slices.DeleteFunc(combinations, func(candidate Combination) bool {
		for _, excluded := range r.exclude {
			if subsetCombination(candidate, excluded) {
				return true
			}
		}
		return false
	})
	for _, inclusion := range r.include {
		added := false
		for _, candidate := range combinations {
			if !compatibleCombination(candidate, inclusion) {
				continue
			}
			for name, item := range inclusion.values {
				if axisIndex(r.axes, name) < 0 {
					candidate.values[name] = item
				}
			}
			added = true
		}
		if !added {
			if len(combinations) >= limit {
				return nil, false
			}
			combinations = append(combinations, inclusion)
		}
	}
	return combinations, true
}

func (r Resolution) Reachable(axisName string) Reach {
	axisName = strings.ToLower(axisName)
	axis := axisIndex(r.axes, axisName)
	known := r.filterKnown && r.state != value.Dynamic && r.state != value.Invalid
	result := Reach{Complete: known}
	if axis >= 0 {
		declaration := r.axes[axis]
		result.Values = slices.Clone(declaration.Values)
		result.Complete = result.Complete && declaration.Complete
	} else {
		result.MayBeMissing = true
		if r.unknownKeys {
			result.Complete = false
		}
	}
	includeCoverage := len(r.include) != 0
	includeSelectsBase := false
	for _, combination := range r.include {
		if item, ok := combination.Get(axisName); ok {
			appendUnique(&result.Values, item)
		} else {
			includeCoverage = false
		}
		for _, baseAxis := range r.axes {
			if _, selected := combination.Get(baseAxis.Name); selected {
				includeSelectsBase = true
			}
		}
	}
	if axis >= 0 {
		if len(r.include) != 0 {
			for _, combination := range r.include {
				if r.includeIsStandalone(combination) {
					if _, supplied := combination.Get(axisName); !supplied {
						result.MayBeMissing = true
					}
				}
			}
		}
	} else {
		result.MayBeMissing = !(includeCoverage && !includeSelectsBase)
	}
	if r.state == value.Dynamic || r.state == value.Invalid || !r.filterKnown {
		result.Complete = false
	}
	if r.unknownKeys {
		result.Complete = false
	}
	return result
}

func (r Resolution) includeIsStandalone(inclusion Combination) bool {
	if len(r.axes) == 0 || !r.filterKnown {
		return true
	}
	witness := Combination{values: map[string]value.Value{}}
	for _, axis := range r.axes {
		if len(axis.Values) == 0 {
			return true
		}
		witness.values[axis.Name] = axis.Values[0]
		if assigned, exists := inclusion.Get(axis.Name); exists {
			matched := false
			for _, candidate := range axis.Values {
				if candidate.Equal(assigned) {
					witness.values[axis.Name] = candidate
					matched = true
					break
				}
			}
			if !matched {
				return true
			}
		}
	}
	return !r.witnessSurvives(witness)
}

func (r Resolution) witnessSurvives(witness Combination) bool {
	for _, exclusion := range r.exclude {
		matches := true
		for name, expected := range exclusion.values {
			actual, ok := witness.Get(name)
			if !ok || !Subset(actual, expected) {
				matches = false
				break
			}
		}
		if matches {
			return false
		}
	}
	return true
}

func (r Resolution) Check(mixedTypes bool) []diagnostic.Diagnostic {
	return checkResolution(r, mixedTypes)
}

func Subset(actual, filter value.Value) bool {
	if actual.State() == value.Dynamic || actual.State() == value.Invalid || filter.State() == value.Dynamic || filter.State() == value.Invalid {
		return true
	}
	if !filter.Known() {
		return false
	}
	switch filter.Kind() {
	case value.Object:
		for name, wanted := range filter.Props() {
			if index, err := matrixIndex(name); err == nil && dynamicIndex(actual, index) {
				return true
			}
			got, ok := lookup(actual, name)
			if !ok {
				got = value.FromJSON(nil, actual.Origin())
			}
			if !Subset(got, wanted) {
				return false
			}
		}
		return true
	case value.Array:
		for i := range filter.Len() {
			wanted, _ := filter.Index(i)
			if dynamic(wanted) {
				return true
			}
			if dynamicIndex(actual, i) {
				return true
			}
			got, ok := lookup(actual, strconv.Itoa(i))
			if !ok {
				got = value.FromJSON(nil, actual.Origin())
			}
			if !Subset(got, wanted) {
				return false
			}
		}
		return true
	default:
		return looseEqual(actual, filter)
	}
}

func lookup(object value.Value, name string) (value.Value, bool) {
	if item, ok := object.Prop(name); ok {
		return item, true
	}
	if object.Kind() == value.Array {
		index, err := matrixIndex(name)
		if err == nil {
			return object.Index(index)
		}
	}
	return value.Value{}, false
}

func matrixIndex(name string) (int, error) {
	if strings.HasPrefix(strings.ToLower(name), "0x") {
		parsed, err := strconv.ParseInt(name[2:], 16, 32)
		return int(parsed), err
	}
	return strconv.Atoi(name)
}

func dynamicIndex(array value.Value, index int) bool {
	if !array.Known() || array.Kind() != value.Array {
		return false
	}
	for i := range min(index+1, array.Len()) {
		item, _ := array.Index(i)
		if dynamic(item) {
			return true
		}
	}
	return false
}

func looseEqual(left, right value.Value) bool {
	if !left.Known() || !right.Known() {
		return false
	}
	if left.Kind() == value.String && right.Kind() == value.String {
		a, _ := left.Text()
		b, _ := right.Text()
		return strings.EqualFold(a, b)
	}
	toNumber := func(item value.Value) (float64, bool) {
		switch item.Kind() {
		case value.Null:
			return 0, true
		case value.Bool:
			b, _ := item.Bool()
			if b {
				return 1, true
			}
			return 0, true
		case value.Number:
			return item.Number()
		case value.String:
			text, _ := item.Text()
			return exprlang.ToNumber(text), true
		default:
			return 0, false
		}
	}
	a, aok := toNumber(left)
	b, bok := toNumber(right)
	return aok && bok && a == b
}

func declarationCombinations(filters *workflowlang.MatrixCombinations, complete bool) ([]Combination, bool) {
	if filters.Expression != nil {
		v := expression(filters.Expression)
		result, ok := semanticCombinations(v, false)
		return result, complete && ok
	}
	var combinations []Combination
	for _, filter := range filters.Combinations {
		if filter.Expression != nil {
			v := expression(filter.Expression)
			inserted, ok := semanticCombinations(v, true)
			if !ok {
				complete = false
				continue
			}
			combinations = append(combinations, inserted...)
			continue
		}
		combination := Combination{values: map[string]value.Value{}}
		for key, assign := range filter.Assigns {
			if assign != nil {
				name := value.YAMLScalar(key, workflowlang.YamlTagStr, value.Origin{})
				if assign.Key != nil {
					name = workflowlang.SemanticValue(assign.Key)
					if filter.Evaluated {
						name = value.YAMLScalar(assign.Key.Value, workflowlang.YamlTagStr, name.Origin())
					}
				}
				resolved, state := mapping.ResolveKey(name)
				if state != mapping.KnownKey {
					complete = false
					continue
				}
				combination.values[strings.ToLower(resolved)] = rawValue(assign.Value, !filter.Evaluated)
			}
		}
		combinations = append(combinations, combination)
	}
	return combinations, complete
}

func semanticCombinations(v value.Value, allowObject bool) ([]Combination, bool) {
	if !v.Known() {
		return nil, false
	}
	if allowObject && v.Kind() == value.Object {
		v = value.Seq(v.Origin(), v)
	}
	if v.Kind() != value.Array {
		return nil, false
	}
	var result []Combination
	for item := range v.Items() {
		if !item.Known() || item.Kind() != value.Object {
			return nil, false
		}
		combination := Combination{values: map[string]value.Value{}}
		for name, field := range item.Props() {
			combination.values[strings.ToLower(name)] = field
		}
		result = append(result, combination)
	}
	return result, true
}

func rawValue(raw workflowlang.RawYAMLValue, expressions bool) value.Value {
	switch node := raw.(type) {
	case *workflowlang.RawYAMLString:
		origin := origin(node.Pos(), expressions)
		if expressions && strings.Contains(node.Value, "${{") {
			return value.Template(node.Value, origin)
		}
		return value.YAMLScalar(node.Value, node.Tag, origin)
	case *workflowlang.RawYAMLArray:
		items := make([]value.Value, 0, len(node.Elems))
		for _, child := range node.Elems {
			scalar, isScalar := child.(*workflowlang.RawYAMLString)
			isExpression := expressions && isScalar && strings.Contains(scalar.Value, "${{")
			item := rawValue(child, isExpression)
			if isExpression && item.Known() && item.Kind() == value.Array {
				for inserted := range item.Items() {
					items = append(items, inserted)
				}
			} else {
				items = append(items, item)
			}
		}
		return value.Seq(origin(node.Pos(), false), items...)
	case *workflowlang.RawYAMLObject:
		props := make([]value.Prop, 0, len(node.Props))
		for _, name := range slices.Sorted(maps.Keys(node.Props)) {
			props = append(props, value.Prop{Name: name, Value: rawValue(node.Props[name], expressions)})
		}
		return value.Map(origin(node.Pos(), false), props...)
	default:
		return value.Value{}
	}
}

func expression(text *workflowlang.String) value.Value {
	if text == nil {
		return value.Value{}
	}
	return value.Template(text.Value, origin(text.Pos, true))
}

func origin(pos *workflowlang.Pos, fromExpression bool) value.Origin {
	o := value.Origin{FromExpression: fromExpression}
	if pos != nil {
		o.Span.Start = diagnostic.Position{Line: pos.Line, Column: pos.Col}
	}
	return o
}

func rowPosition(row *workflowlang.MatrixRow) *workflowlang.Pos {
	if row.Expression != nil {
		return row.Expression.Pos
	}
	if len(row.Values) != 0 {
		return row.Values[0].Pos()
	}
	if row.Name != nil {
		return row.Name.Pos
	}
	return nil
}

func axisIndex(axes []Axis, name string) int {
	for i, axis := range axes {
		if strings.EqualFold(axis.Name, name) {
			return i
		}
	}
	return -1
}

func appendUnique(values *[]value.Value, candidate value.Value) {
	for _, existing := range *values {
		if existing.Equal(candidate) {
			return
		}
	}
	*values = append(*values, candidate)
}

func subsetCombination(actual, expected Combination) bool {
	for name, filter := range expected.values {
		got, ok := actual.values[name]
		if ok && !Subset(got, filter) {
			return false
		}
	}
	return true
}

func compatibleCombination(actual, expected Combination) bool {
	for name, filter := range expected.values {
		if got, ok := actual.values[name]; ok && !Subset(got, filter) {
			return false
		}
	}
	return true
}
