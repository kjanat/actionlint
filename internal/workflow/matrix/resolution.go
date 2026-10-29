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

type Combination struct {
	values     map[string]value.Value
	keyOrigins map[string]value.Origin
	origin     value.Origin
}

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
	name = strings.ToLower(name)
	values := maps.Clone(c.values)
	if values == nil {
		values = map[string]value.Value{}
	}
	keyOrigins := maps.Clone(c.keyOrigins)
	if keyOrigins == nil {
		keyOrigins = map[string]value.Origin{}
	}
	if _, declared := keyOrigins[name]; !declared {
		keyOrigins[name] = item.Origin()
	}
	values[name] = item
	return Combination{values: values, keyOrigins: keyOrigins, origin: c.origin}
}

func (c Combination) keyOrigin(name string) value.Origin {
	if origin, declared := c.keyOrigins[strings.ToLower(name)]; declared {
		return origin
	}
	item, _ := c.Get(name)
	return item.Origin()
}

type Reach struct {
	Values       []value.Value
	MayBeMissing bool
	Complete     bool
}

type exclusionIssue interface{ exclusionIssue() }

type exclusionWithoutAxes struct{ origin value.Origin }

func (exclusionWithoutAxes) exclusionIssue() {}

type emptyExclusion struct{ origin value.Origin }

func (emptyExclusion) exclusionIssue() {}

type unknownExclusionAxis struct {
	name   string
	origin value.Origin
}

func (unknownExclusionAxis) exclusionIssue() {}

type Resolution struct {
	state        value.State
	axes         []Axis
	include      []Combination
	exclude      []Combination
	filterKnown  bool
	includeKnown bool
	excludeKnown bool
	hasInclude   bool
	hasExclude   bool
	unknownKeys  bool
	origin       value.Origin
}

func (r Resolution) State() value.State { return r.state }

// IncludesComplete reports whether all added combinations and their keys are known.
// Their values may still be dynamic; Reachable retains those values explicitly.
func (r Resolution) IncludesComplete() bool { return r.includeKnown }

func (r Resolution) Open() bool {
	return r.unknownKeys || r.state == value.Dynamic || r.state == value.Invalid
}

func (r Resolution) exclusionIssues() iter.Seq[exclusionIssue] {
	return func(yield func(exclusionIssue) bool) {
		if len(r.exclude) == 0 {
			return
		}
		for _, filter := range r.exclude {
			if len(filter.values) == 0 && !yield(emptyExclusion{origin: filter.origin}) {
				return
			}
		}
		if r.unknownKeys || r.state == value.Dynamic {
			return
		}
		if len(r.axes) == 0 {
			yield(exclusionWithoutAxes{origin: r.origin})
			return
		}
		for _, filter := range r.exclude {
			for name := range filter.Values() {
				if axisIndex(r.axes, name) < 0 && !yield(unknownExclusionAxis{name: name, origin: filter.keyOrigin(name)}) {
					return
				}
			}
		}
	}
}

func (r Resolution) hasExclusionIssues() bool {
	for range r.exclusionIssues() {
		return true
	}
	return false
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
		return Resolution{state: value.Missing, filterKnown: true, includeKnown: true, excludeKnown: true}
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
		return Resolution{state: value.Missing, filterKnown: true, includeKnown: true, excludeKnown: true}
	}
	return resolveDeclaration(strategy.Matrix)
}

func resolveDeclaration(matrix *workflowlang.Matrix) Resolution {
	if matrix.Expression != nil {
		return resolveSemanticMatrix(expression(matrix.Expression))
	}
	r := Resolution{state: value.Literal, filterKnown: true, includeKnown: true, excludeKnown: true, origin: origin(matrix.Pos, false)}
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
		if r.hasInclude && part.hasInclude || r.hasExclude && part.hasExclude {
			r.state, r.filterKnown, r.includeKnown = value.Invalid, false, false
		}
		if r.hasExclude && part.hasExclude {
			r.excludeKnown = false
		}
		r.hasInclude = r.hasInclude || part.hasInclude
		r.hasExclude = r.hasExclude || part.hasExclude
		r.include = append(r.include, part.include...)
		r.exclude = append(r.exclude, part.exclude...)
		r.filterKnown = r.filterKnown && part.filterKnown
		r.includeKnown = r.includeKnown && part.includeKnown
		r.excludeKnown = r.excludeKnown && part.excludeKnown
		r.unknownKeys = r.unknownKeys || part.unknownKeys
	}
	if matrix.Include != nil {
		if r.hasInclude {
			r.state, r.filterKnown, r.includeKnown = value.Invalid, false, false
		}
		r.hasInclude = true
		included, complete := declarationCombinations(matrix.Include, true)
		r.include = append(r.include, included...)
		r.includeKnown = r.includeKnown && complete
		r.filterKnown = r.filterKnown && complete
	}
	if matrix.Exclude != nil {
		if r.hasExclude {
			r.state, r.filterKnown, r.includeKnown = value.Invalid, false, false
			r.excludeKnown = false
		}
		r.hasExclude = true
		excluded, complete := declarationCombinations(matrix.Exclude, true)
		r.exclude = append(r.exclude, excluded...)
		r.filterKnown = r.filterKnown && complete
		r.excludeKnown = r.excludeKnown && complete
	}
	return r
}

func resolveSemanticMatrix(matrix value.Value) Resolution {
	r := Resolution{state: matrix.State(), filterKnown: matrix.Known(), includeKnown: matrix.Known(), excludeKnown: matrix.Known(), origin: matrix.Origin()}
	if !matrix.Known() {
		r.unknownKeys = true
		return r
	}
	if matrix.Kind() != value.Object {
		r.state, r.filterKnown = value.Invalid, false
		return r
	}
	for name, row := range matrix.Props() {
		if strings.EqualFold(name, "include") || strings.EqualFold(name, "exclude") {
			filters, ok := semanticCombinations(row, false)
			if !ok {
				r.filterKnown = false
			}
			if strings.EqualFold(name, "include") {
				if r.hasInclude {
					r.state, r.filterKnown, r.includeKnown = value.Invalid, false, false
				}
				r.hasInclude = true
				r.includeKnown = r.includeKnown && ok
				r.include = append(r.include, filters...)
			} else {
				if r.hasExclude {
					r.state, r.filterKnown, r.includeKnown = value.Invalid, false, false
					r.excludeKnown = false
				}
				r.hasExclude = true
				r.excludeKnown = r.excludeKnown && ok
				r.exclude = append(r.exclude, filters...)
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
	return r.combinations(limit, false)
}

// DeclarationCombinations retains excluded original declarations alongside the
// execution combinations. Includes still merge only into surviving originals;
// validation cannot turn a standalone include into a phantom merged job.
func (r Resolution) DeclarationCombinations(limit int) ([]Combination, bool) {
	return r.combinations(limit, true)
}

func (r Resolution) combinations(limit int, declarations bool) ([]Combination, bool) {
	if r.state == value.Missing {
		return []Combination{{values: map[string]value.Value{}}}, true
	}
	if r.state == value.Dynamic || r.state == value.Invalid || r.unknownKeys || !r.filterKnown || r.hasExclusionIssues() || limit < 0 {
		return nil, false
	}
	for _, exclusion := range r.exclude {
		for _, expected := range exclusion.values {
			if dynamic(expected) {
				return nil, false
			}
		}
	}
	for _, inclusion := range r.include {
		for name, expected := range inclusion.values {
			if axisIndex(r.axes, name) >= 0 && dynamic(expected) {
				return nil, false
			}
		}
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
				if dynamic(item) {
					return nil, false
				}
				next = append(next, combination.With(axis.Name, item))
			}
		}
		combinations = next
	}
	var excludedDeclarations []Combination
	kept := combinations[:0]
	for _, candidate := range combinations {
		if !r.excludes(candidate) {
			kept = append(kept, candidate)
		} else if declarations {
			excludedDeclarations = append(excludedDeclarations, candidate)
		}
	}
	combinations = kept
	originalCount := len(combinations)
	for _, inclusion := range r.include {
		added := false
		for _, candidate := range combinations[:originalCount] {
			match := includeMatch(candidate, inclusion, r.axes)
			if match == matchUncertain {
				return nil, false
			}
			if match != matchAlways {
				continue
			}
			for name, item := range inclusion.values {
				if axisIndex(r.axes, name) < 0 {
					candidate.values[name] = item
					candidate.keyOrigins[name] = inclusion.keyOrigin(name)
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
	if len(combinations)+len(excludedDeclarations) > limit {
		return nil, false
	}
	combinations = append(combinations, excludedDeclarations...)
	return combinations, true
}

// excludes reports whether any exclude entry removes candidate.
func (r Resolution) excludes(candidate Combination) bool {
	for _, excluded := range r.exclude {
		if exclusionMatches(candidate.exclusionValues, excluded) {
			return true
		}
	}
	return false
}

func (r Resolution) Reachable(axisName string) Reach {
	axisName = strings.ToLower(axisName)
	axis := axisIndex(r.axes, axisName)
	known := r.filterKnown && r.state != value.Dynamic && r.state != value.Invalid && !r.hasExclusionIssues()
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
		result.MayBeMissing = !includeCoverage || includeSelectsBase
	}
	if r.state == value.Dynamic || r.state == value.Invalid || !r.filterKnown {
		result.Complete = false
	}
	if r.unknownKeys {
		result.Complete = false
	}
	return result
}

// ReachableAfterExcludes narrows base-axis alternatives by definite exclusions,
// retaining all include assignments. Large or dynamic axes use partial proofs.
func (r Resolution) ReachableAfterExcludes(axisName string) Reach {
	return r.reachableAlternatives(axisName, true)
}

// DeclarationReachable preserves original axis declarations while retaining the
// same proof of whether an include supplies a separate alternative as execution.
func (r Resolution) DeclarationReachable(axisName string) Reach {
	return r.reachableAlternatives(axisName, false)
}

func (r Resolution) reachableAlternatives(axisName string, excludeOriginals bool) Reach {
	axisName = strings.ToLower(axisName)
	result := r.Reachable(axisName)
	if r.state == value.Invalid || r.state == value.Dynamic || r.hasExclusionIssues() {
		return result
	}
	axis := axisIndex(r.axes, axisName)
	if axis < 0 {
		return result
	}
	result.Values = nil
	var surviving []Combination
	var enumerable bool
	if excludeOriginals {
		surviving, enumerable = r.survivingBaseCombinations(256)
	}
	for _, candidate := range r.axes[axis].Values {
		excluded := excludeOriginals && r.definitelyExcludedAxisValue(axisName, candidate)
		if enumerable {
			excluded = !slices.ContainsFunc(surviving, func(combination Combination) bool {
				item, exists := combination.Get(axisName)
				return exists && item.Equal(candidate)
			})
		}
		if !excluded {
			result.Values = append(result.Values, candidate)
		}
	}
	for _, inclusion := range r.include {
		if item, exists := inclusion.Get(axisName); exists && r.includeApplicability(inclusion) != matchAlways {
			appendUnique(&result.Values, item)
		}
	}
	return result
}

func (r Resolution) survivingBaseCombinations(limit int) ([]Combination, bool) {
	base := r
	base.include = nil
	base.filterKnown = true
	base.exclude = slices.DeleteFunc(slices.Clone(r.exclude), func(exclusion Combination) bool {
		for _, expected := range exclusion.values {
			if dynamic(expected) {
				return true
			}
		}
		return false
	})
	return base.Combinations(limit)
}

// A fallback axis value is unreachable when one known exclusion covers it
// regardless of the other axes. Conditional or dynamic exclusions prove less.
func (r Resolution) definitelyExcludedAxisValue(axisName string, candidate value.Value) bool {
	for _, exclusion := range r.exclude {
		lookup := func(name string) ([]value.Value, bool) {
			if name == axisName {
				return []value.Value{candidate}, true
			}
			index := axisIndex(r.axes, name)
			if index < 0 || !r.axes[index].Complete {
				return nil, false
			}
			return r.axes[index].Values, true
		}
		if exclusionMatches(lookup, exclusion) {
			return true
		}
	}
	return false
}

func (r Resolution) includeIsStandalone(inclusion Combination) bool {
	return r.includeApplicability(inclusion) != matchAlways
}

// includeApplicability proves whether an include merges into a surviving
// original. Other include entries never change original axis values.
func (r Resolution) includeApplicability(inclusion Combination) matchProof {
	if r.Open() || !r.excludeKnown || r.hasExclusionIssues() {
		return matchUncertain
	}
	base := r
	base.include = nil
	base.filterKnown = r.excludeKnown
	if combinations, complete := base.Combinations(256); complete {
		result := matchNever
		for _, candidate := range combinations {
			switch includeMatch(candidate, inclusion, r.axes) {
			case matchAlways:
				return matchAlways
			case matchUncertain:
				result = matchUncertain
			case matchNever:
			}
		}
		return result
	}
	witness := Combination{values: map[string]value.Value{}}
	for _, axis := range r.axes {
		matched := false
		for _, candidate := range axis.Values {
			if !candidate.Known() || dynamic(candidate) {
				continue
			}
			part := (Combination{}).With(axis.Name, candidate)
			if includeMatch(part, inclusion, []Axis{axis}) == matchAlways {
				witness.values[axis.Name] = candidate
				matched = true
				break
			}
		}
		if !matched {
			return matchUncertain
		}
	}
	if r.witnessSurvives(witness) {
		return matchAlways
	}
	return matchUncertain
}

func (r Resolution) witnessSurvives(witness Combination) bool {
	if !r.excludeKnown {
		return false
	}
	for _, exclusion := range r.exclude {
		if exclusionMatch(witness.exclusionValues, exclusion) != matchNever {
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
	case value.KindUnknown, value.Null, value.Bool, value.Number, value.String:
	}
	return looseEqual(actual, filter)
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
		case value.KindUnknown, value.Array, value.Object:
		}
		return 0, false
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
		combination := Combination{values: map[string]value.Value{}, keyOrigins: map[string]value.Origin{}}
		if filters.Name != nil {
			combination.origin = origin(filters.Name.Pos, false)
		}
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
				resolved = strings.ToLower(resolved)
				combination.values[resolved] = rawValue(assign.Value, !filter.Evaluated)
				combination.keyOrigins[resolved] = name.Origin()
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
		combination := Combination{values: map[string]value.Value{}, keyOrigins: map[string]value.Origin{}, origin: item.Origin()}
		for name, field := range item.Props() {
			name = strings.ToLower(name)
			combination.values[name] = field
			combination.keyOrigins[name] = item.Origin()
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
			item := rawValue(child, expressions)
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

func (c Combination) exclusionValues(name string) ([]value.Value, bool) {
	item, exists := c.Get(name)
	return []value.Value{item}, exists
}

type matchProof uint8

const (
	matchNever matchProof = iota
	matchUncertain
	matchAlways
)

func exclusionMatches(actual func(string) ([]value.Value, bool), expected Combination) bool {
	return exclusionMatch(actual, expected) == matchAlways
}

// exclusionMatch checks every supplied candidate for each key. A known
// mismatch disproves coverage; dynamic values cannot prove a witness survives.
func exclusionMatch(actual func(string) ([]value.Value, bool), expected Combination) matchProof {
	if len(expected.values) == 0 {
		return matchNever
	}
	result := matchAlways
	for name, filter := range expected.values {
		candidates, complete := actual(name)
		if !complete || len(candidates) == 0 {
			return matchNever
		}
		if dynamic(filter) {
			result = matchUncertain
			continue
		}
		for _, got := range candidates {
			if !got.Known() || dynamic(got) {
				result = matchUncertain
				continue
			}
			if !Subset(got, filter) {
				return matchNever
			}
		}
	}
	return result
}

func includeMatch(actual, expected Combination, axes []Axis) matchProof {
	result := matchAlways
	for name, filter := range expected.values {
		if axisIndex(axes, name) < 0 {
			continue
		}
		got, exists := actual.Get(name)
		if !exists || !got.Known() || dynamic(got) || dynamic(filter) {
			result = matchUncertain
			continue
		}
		if !Subset(got, filter) {
			return matchNever
		}
	}
	return result
}
