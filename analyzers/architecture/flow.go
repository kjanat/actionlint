package architecture

import (
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

type origins map[int]bool

// Negative indices carry uncertainty from a call whose implementation is absent.
// They are not presented as proven aliases, and are not silently accepted.
func uncertain(from origins) origins {
	out := origins{}
	for index := range from {
		if index >= 0 {
			index = -index - 1
		}
		out[index] = true
	}
	return out
}

func (to origins) add(from origins) bool {
	changed := false
	for origin := range from {
		if !to[origin] {
			to[origin] = true
			changed = true
		}
	}
	return changed
}

// yields records what a function passes to its function-typed parameters, such
// as an iterator's yield function. Opaque is set when one of those parameters
// is used other than by calling it, for example stored or passed on: what it
// receives is then unknown.
type yields struct {
	From   origins
	Opaque bool
}

func (y *yields) add(other yields) bool {
	changed := y.From.add(other.From)
	if other.Opaque && !y.Opaque {
		y.Opaque = true
		changed = true
	}
	return changed
}

type flow struct {
	Results []origins
	Writes  origins
	// Stores maps a destination parameter to the origins stored into its
	// storage, so a caller can carry a source's aliases into the destination.
	Stores map[int]origins
	// Yields is what this function passes to its function-typed parameters.
	// It is a pointer so that merging into a summary held in a map by value
	// keeps its Opaque flag.
	Yields *yields
	// ResultYields is, for each function-typed result, what that function
	// passes to its own function-typed parameters when it is called, in this
	// function's origins. An iterator returned by a method yields the entries
	// listed here to the loop body that ranges over it.
	ResultYields []yields
}

type functionFlow struct {
	Name    string
	Results [][]int
	Writes  []int
	// Stores pairs a destination parameter with one stored origin.
	Stores             [][2]int
	Yields             []int
	YieldsOpaque       bool
	ResultYields       [][]int
	ResultYieldsOpaque []bool
}

// Imported helpers carry parameter-to-result and write effects. Renaming or
// moving a helper does not change how its callers are checked.
type flowFact struct{ Functions []functionFlow }

func (*flowFact) AFact()         {}
func (*flowFact) String() string { return "function flow summaries" }

func newFlow(results int) flow {
	f := flow{
		Results:      make([]origins, results),
		Writes:       origins{},
		Stores:       map[int]origins{},
		Yields:       &yields{From: origins{}},
		ResultYields: make([]yields, results),
	}
	for i := range f.Results {
		f.Results[i] = origins{}
		f.ResultYields[i] = yields{From: origins{}}
	}
	return f
}

func (f flow) add(other flow) bool {
	changed := f.Writes.add(other.Writes)
	changed = f.Yields.add(*other.Yields) || changed
	for i := range f.ResultYields {
		changed = f.ResultYields[i].add(other.ResultYields[i]) || changed
	}
	for destination, sources := range other.Stores {
		if f.Stores[destination] == nil {
			f.Stores[destination] = origins{}
		}
		changed = f.Stores[destination].add(sources) || changed
	}
	for i := range f.Results {
		changed = f.Results[i].add(other.Results[i]) || changed
	}
	return changed
}

func functionKey(obj *types.Func) string { return obj.Origin().FullName() }

func solve(pass *analysis.Pass, functions []*ssa.Function) map[*ssa.Function]flow {
	// Bodyless declarations (assembly, linkname) have unknown effects. Leaving
	// them unsummarized sends their callers down the unknown-call path.
	functions = slices.DeleteFunc(slices.Clone(functions), func(fn *ssa.Function) bool { return fn.Blocks == nil })
	flows := map[*ssa.Function]flow{}
	for _, fn := range functions {
		flows[fn] = newFlow(fn.Signature.Results().Len())
	}
	imported := map[*types.Package]flowFact{}
	lookup := func(fn *ssa.Function) (flow, bool) {
		if f, found := flows[fn]; found {
			return f, true
		}
		obj, _ := fn.Object().(*types.Func)
		if obj == nil || obj.Pkg() == nil {
			return flow{}, false
		}
		facts, found := imported[obj.Pkg()]
		if !found {
			pass.ImportPackageFact(obj.Pkg(), &facts)
			imported[obj.Pkg()] = facts
		}
		index := slices.IndexFunc(facts.Functions, func(f functionFlow) bool { return f.Name == functionKey(obj) })
		if index < 0 {
			return flow{}, false
		}
		encoded := facts.Functions[index]
		// A bound method wrapper (a method value) shares the method's object,
		// but takes the method's parameters as its own and its receiver as its
		// only free variable: summary origin 0 is wrapper origin len(Params),
		// and origin i > 0 is wrapper origin i-1.
		at := func(origin int) int { return origin }
		if obj.Signature().Recv() != nil && fn.Signature.Recv() == nil && len(fn.FreeVars) == 1 {
			at = func(origin int) int {
				possible := origin < 0
				if possible {
					origin = -origin - 1
				}
				if origin == 0 {
					origin = len(fn.Params)
				} else {
					origin--
				}
				if possible {
					origin = -origin - 1
				}
				return origin
			}
		}
		f := newFlow(len(encoded.Results))
		for i, values := range encoded.Results {
			for _, value := range values {
				f.Results[i][at(value)] = true
			}
		}
		for _, value := range encoded.Writes {
			f.Writes[at(value)] = true
		}
		for _, pair := range encoded.Stores {
			destination := at(pair[0])
			if f.Stores[destination] == nil {
				f.Stores[destination] = origins{}
			}
			f.Stores[destination][at(pair[1])] = true
		}
		for _, value := range encoded.Yields {
			f.Yields.From[at(value)] = true
		}
		f.Yields.Opaque = encoded.YieldsOpaque
		for i, values := range encoded.ResultYields {
			for _, value := range values {
				f.ResultYields[i].From[at(value)] = true
			}
			f.ResultYields[i].Opaque = encoded.ResultYieldsOpaque[i]
		}
		return f, true
	}
	for changed := true; changed; {
		changed = false
		// Visibility grows with the summaries. A round may use a stale answer:
		// any summary change forces another round with a fresh cache, so the
		// final round agrees with the final summaries.
		visibility := map[types.Type]bool{}
		visible := func(t types.Type) bool {
			known, found := visibility[t]
			if !found {
				known = mutable(t) || storage(t) && mutatingAPI(pass, t, flows, map[types.Type]bool{}) != apiReadOnly
				visibility[t] = known
			}
			return known
		}
		for _, fn := range functions {
			changed = flows[fn].add(analyzeFlow(fn, lookup, visible)) || changed
		}
	}
	facts := flowFact{}
	for fn, f := range flows {
		obj, _ := fn.Object().(*types.Func)
		if obj == nil {
			continue
		}
		encoded := functionFlow{Name: functionKey(obj), Results: make([][]int, len(f.Results))}
		for i, values := range f.Results {
			for value := range values {
				encoded.Results[i] = append(encoded.Results[i], value)
			}
			slices.Sort(encoded.Results[i])
		}
		for value := range f.Writes {
			encoded.Writes = append(encoded.Writes, value)
		}
		slices.Sort(encoded.Writes)
		for destination, sources := range f.Stores {
			for source := range sources {
				encoded.Stores = append(encoded.Stores, [2]int{destination, source})
			}
		}
		slices.SortFunc(encoded.Stores, func(a, b [2]int) int {
			if a[0] != b[0] {
				return a[0] - b[0]
			}
			return a[1] - b[1]
		})
		encoded.Yields = sortedOrigins(f.Yields.From)
		encoded.YieldsOpaque = f.Yields.Opaque
		for _, y := range f.ResultYields {
			encoded.ResultYields = append(encoded.ResultYields, sortedOrigins(y.From))
			encoded.ResultYieldsOpaque = append(encoded.ResultYieldsOpaque, y.Opaque)
		}
		facts.Functions = append(facts.Functions, encoded)
	}
	slices.SortFunc(facts.Functions, func(a, b functionFlow) int { return strings.Compare(a.Name, b.Name) })
	pass.ExportPackageFact(&facts)
	return flows
}

func sortedOrigins(from origins) []int {
	out := make([]int, 0, len(from))
	for origin := range from {
		out = append(out, origin)
	}
	slices.Sort(out)
	return out
}

// mutable reports storage a caller can modify through a value. Private fields
// in value handles do not expose storage. A slice of such values is still mutable.
func mutable(t types.Type) bool { return mutableType(t, map[types.Type]bool{}, false) }
func storage(t types.Type) bool { return mutableType(t, map[types.Type]bool{}, true) }
func mutableType(t types.Type, seen map[types.Type]bool, private bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	t = types.Unalias(t)
	switch t := t.(type) {
	case *types.Named:
		return mutableType(t.Underlying(), seen, private)
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return true
	case *types.TypeParam:
		// Every permitted instantiation must be a value type for the parameter
		// to hold no storage. Constraints without type terms permit any type.
		terms, restricted := typeTerms(t.Constraint(), map[types.Type]bool{})
		if !restricted {
			return true
		}
		for _, term := range terms {
			if mutableType(term, seen, private) {
				return true
			}
		}
		return false
	case *types.Basic:
		return t.Kind() == types.UnsafePointer
	case *types.Array:
		return mutableType(t.Elem(), seen, private)
	case *types.Struct:
		for field := range t.Fields() {
			if (private || field.Exported()) && mutableType(field.Type(), seen, private) {
				return true
			}
		}
	case *types.Tuple:
		for v := range t.Variables() {
			if mutableType(v.Type(), seen, private) {
				return true
			}
		}
	}
	return false
}

// typeTerms lists the type terms of a constraint. restricted is false when the
// type set is not limited by terms, as with any, comparable or method sets.
// Terms of embedded interfaces intersect; any restricted one bounds the set.
func typeTerms(constraint types.Type, seen map[types.Type]bool) (terms []types.Type, restricted bool) {
	iface, ok := constraint.Underlying().(*types.Interface)
	if !ok || seen[constraint] {
		return nil, false
	}
	seen[constraint] = true
	for embedded := range iface.EmbeddedTypes() {
		switch embedded := embedded.(type) {
		case *types.Union:
			for term := range embedded.Terms() {
				nested, ok := typeTerms(term.Type(), seen)
				if !ok {
					terms = append(terms, term.Type())
				}
				terms = append(terms, nested...)
			}
			return terms, true
		default:
			if _, isInterface := embedded.Underlying().(*types.Interface); isInterface {
				if nested, ok := typeTerms(embedded, seen); ok {
					return nested, true
				}
				continue
			}
			return []types.Type{embedded}, true
		}
	}
	return nil, false
}

// apiEffect orders what a value's exported methods may do to its storage.
type apiEffect int

const (
	apiReadOnly apiEffect = iota
	// apiUnresolved means a method has no summary, as with standard-library
	// methods, whose bodies are not analyzed. It is not proof of immutability.
	apiUnresolved
	apiMutating
)

// A private representation is not an immutable handle when its exported
// methods can mutate it. Use the same source effects as the field check.
func mutatingAPI(pass *analysis.Pass, t types.Type, flows map[*ssa.Function]flow, seen map[types.Type]bool) apiEffect {
	if seen[t] {
		return apiReadOnly
	}
	seen[t] = true
	t = types.Unalias(t)
	effect := apiReadOnly
	if named, ok := t.(*types.Named); ok {
		methods := types.NewMethodSet(types.NewPointer(named))
		for method := range methods.Methods() {
			method, isFunc := method.Obj().(*types.Func)
			if !isFunc || !method.Exported() {
				continue
			}
			summarized := false
			for fn, local := range flows {
				if obj, ok := fn.Object().(*types.Func); ok && functionKey(obj) == functionKey(method) {
					summarized = true
					if local.Writes[0] || local.Writes[-1] {
						return apiMutating
					}
				}
			}
			if !summarized && method.Pkg() != pass.Pkg {
				var imported flowFact
				if pass.ImportPackageFact(method.Pkg(), &imported) {
					for _, summary := range imported.Functions {
						if summary.Name == functionKey(method) {
							summarized = true
							if slices.Contains(summary.Writes, 0) || slices.Contains(summary.Writes, -1) {
								return apiMutating
							}
						}
					}
				}
			}
			if !summarized {
				effect = apiUnresolved
			}
		}
		return max(effect, mutatingAPI(pass, named.Underlying(), flows, seen))
	}
	if structure, ok := t.(*types.Struct); ok {
		for field := range structure.Fields() {
			if field.Exported() {
				effect = max(effect, mutatingAPI(pass, field.Type(), flows, seen))
			}
		}
	}
	if array, ok := t.(*types.Array); ok {
		return mutatingAPI(pass, array.Elem(), flows, seen)
	}
	return effect
}

func analyzeFlow(fn *ssa.Function, lookup func(*ssa.Function) (flow, bool), visible func(types.Type) bool) flow {
	result := newFlow(fn.Signature.Results().Len())
	values := map[ssa.Value]origins{}
	get := func(value ssa.Value) origins {
		if value == nil {
			return nil
		}
		if values[value] == nil {
			values[value] = origins{}
		}
		return values[value]
	}
	// A receiver is a parameter like any other: a value receiver whose type
	// holds no storage is a private copy and cannot alias the caller's value.
	for i, p := range fn.Params {
		if storage(p.Type()) {
			get(p)[i] = true
		}
	}
	for i, p := range fn.FreeVars {
		if storage(p.Type()) {
			get(p)[len(fn.Params)+i] = true
		}
	}
	// A function-typed parameter that is only ever called receives exactly the
	// arguments of those calls, recorded below. Any other use, such as storing
	// it or passing it on, hides what it receives.
	callbackParams := map[ssa.Value]bool{}
	for _, p := range fn.Params {
		if _, ok := p.Type().Underlying().(*types.Signature); !ok {
			continue
		}
		callbackParams[p] = true
		for _, referrer := range *p.Referrers() {
			if _, debug := referrer.(*ssa.DebugRef); debug {
				continue
			}
			call, ok := referrer.(ssa.CallInstruction)
			if !ok || call.Common().Value != p || slices.Contains(call.Common().Args, ssa.Value(p)) {
				result.Yields.Opaque = true
			}
		}
	}
	tuples := map[ssa.Value][]origins{}
	var root func(ssa.Value) ssa.Value
	root = func(v ssa.Value) ssa.Value {
		switch v := v.(type) {
		case *ssa.FieldAddr:
			return root(v.X)
		case *ssa.IndexAddr:
			return root(v.X)
		case *ssa.Slice:
			return root(v.X)
		case *ssa.ChangeType:
			return root(v.X)
		case *ssa.Convert:
			return root(v.X)
		default:
			return v
		}
	}
	// fieldValue finds the value that field index of allocation holds at
	// instruction at, from stores in blocks dominating it. A store of the whole
	// aggregate copied from another local allocation, as SSA emits for
	// `v := T{f: make(...)}`, takes that allocation's field at the copy.
	var fieldValue func(allocation *ssa.Alloc, index int, at ssa.Instruction) (ssa.Value, bool)
	fieldValue = func(allocation *ssa.Alloc, index int, at ssa.Instruction) (ssa.Value, bool) {
		var blocks []*ssa.BasicBlock
		for block := at.Block(); block != nil; block = block.Idom() {
			blocks = append(blocks, block)
		}
		slices.Reverse(blocks)
		var value ssa.Value
		for _, block := range fn.Blocks {
			if block.Dominates(at.Block()) {
				continue
			}
			for _, instruction := range block.Instrs {
				if store, ok := instruction.(*ssa.Store); ok && root(store.Addr) == allocation {
					return nil, false
				}
			}
		}
		for _, block := range blocks {
			for _, instruction := range block.Instrs {
				if instruction == at {
					break
				}
				store, ok := instruction.(*ssa.Store)
				if !ok {
					continue
				}
				if store.Addr == allocation {
					value = nil
					if copied, ok := store.Val.(*ssa.UnOp); ok && copied.Op == token.MUL {
						if source, ok := copied.X.(*ssa.Alloc); ok {
							value, _ = fieldValue(source, index, copied)
						}
					}
				}
				if address, ok := store.Addr.(*ssa.FieldAddr); ok && address.X == allocation && address.Field == index {
					value = store.Val
				}
			}
		}
		return value, value != nil
	}
	storedField := func(load *ssa.UnOp) (ssa.Value, bool) {
		field, ok := load.X.(*ssa.FieldAddr)
		if !ok {
			return nil, false
		}
		allocation, ok := field.X.(*ssa.Alloc)
		if !ok {
			return nil, false
		}
		return fieldValue(allocation, field.Field, load)
	}
	// freshOuter reports outer storage allocated by this call. A value already
	// in seen is being decided by an enclosing query; a phi is fresh when every
	// edge is, so a revisited value (a loop back edge or a shared edge) defers
	// to the remaining edges instead of deciding the result.
	var freshOuter func(ssa.Value, map[ssa.Value]bool) bool
	freshOuter = func(value ssa.Value, seen map[ssa.Value]bool) bool {
		if seen[value] {
			return true
		}
		seen[value] = true
		switch value := root(value).(type) {
		case *ssa.Alloc, *ssa.MakeMap, *ssa.MakeSlice:
			return true
		case *ssa.Const:
			// A nil slice or map shares no storage; writing a nil map panics.
			return true
		case *ssa.Phi:
			for _, edge := range value.Edges {
				if !freshOuter(edge, seen) {
					return false
				}
			}
			return true
		case *ssa.UnOp:
			if stored, ok := storedField(value); ok {
				return freshOuter(stored, seen)
			}
		case *ssa.Call:
			if callee := value.Common().StaticCallee(); callee != nil {
				return standardClone(callee)
			}
		}
		return false
	}
	write := func(address ssa.Value) {
		// Payload aliases stored inside a fresh aggregate do not make the
		// aggregate's own fields alias the receiver. Loading a pointer from
		// it still carries that pointer's origin through UnOp below.
		if freshOuter(address, map[ssa.Value]bool{}) {
			return
		}
		result.Writes.add(get(address))
	}
	possibleWrite := func(address ssa.Value) {
		if storage(address.Type()) {
			result.Writes.add(uncertain(get(address)))
		}
	}
	// unknownWrite records that code without a summary may write argument.
	// An argument whose type is a read-only value handle is not recorded: the
	// analyzer assumes such code changes an argument only through its exported
	// fields and methods. A loop body in this package that writes the handles
	// it receives is handled separately at the call.
	unknownWrite := func(argument ssa.Value) {
		if visible(argument.Type()) {
			result.Writes.add(uncertain(get(argument)))
		}
	}
	// captures reports whether a closure built from fn exposes its binding k
	// to code that calls the closure. It does when the binding reaches a
	// result of a visible type, when the closure writes it (passing it with a
	// visible type to code without a summary, such as an iterator's yield
	// function, counts as a write), or when the closure stores it into a
	// parameter. A binding the closure only reads, or passes on inside a
	// read-only value handle, is not exposed.
	captures := func(fn *ssa.Function, summary flow, k int) bool {
		index := len(fn.Params) + k
		reaches := func(from origins) bool { return from[index] || from[-index-1] }
		for i, results := range summary.Results {
			if reaches(results) && visible(fn.Signature.Results().At(i).Type()) {
				return true
			}
		}
		if reaches(summary.Writes) {
			return true
		}
		for _, sources := range summary.Stores {
			if reaches(sources) {
				return true
			}
		}
		return false
	}
	// yieldsOf reports what calling the function value passes to its
	// function-typed parameters, in this function's origins. ok is false when
	// that is unknown: the value is not a closure literal, a source function or
	// the result of a summarized call, or it may also pass storage that came
	// from its own parameters.
	var yieldsOf func(value ssa.Value, seen map[ssa.Value]bool) (origins, bool)
	yieldsOf = func(value ssa.Value, seen map[ssa.Value]bool) (origins, bool) {
		if seen[value] {
			return origins{}, true
		}
		seen[value] = true
		// remap translates callee origins to this function's values: indices
		// below len(params) name params, the rest name bindings.
		remap := func(from origins, params []ssa.Value, bindings []ssa.Value) (origins, bool) {
			out := origins{}
			for index := range from {
				possible := index < 0
				if possible {
					index = -index - 1
				}
				var source ssa.Value
				switch {
				case index < len(params):
					source = params[index]
				case index-len(params) < len(bindings):
					source = bindings[index-len(params)]
				default:
					return nil, false
				}
				if source == nil {
					return nil, false
				}
				aliases := get(source)
				if possible {
					aliases = uncertain(aliases)
				}
				out.add(aliases)
			}
			return out, true
		}
		switch value := value.(type) {
		case *ssa.MakeClosure:
			body, _ := value.Fn.(*ssa.Function)
			if body == nil {
				return nil, false
			}
			summary, found := lookup(body)
			if !found || summary.Yields.Opaque {
				return nil, false
			}
			// The closure's own parameters are supplied by whoever calls it,
			// so a yield of one of them is unknown here.
			return remap(summary.Yields.From, make([]ssa.Value, len(body.Params)), value.Bindings)
		case *ssa.Function:
			summary, found := lookup(value)
			if !found || summary.Yields.Opaque {
				return nil, false
			}
			return remap(summary.Yields.From, make([]ssa.Value, len(value.Params)), nil)
		case *ssa.Call:
			common := value.Common()
			callee := common.StaticCallee()
			if callee == nil {
				return nil, false
			}
			// maps.Keys and the like yield their argument's storage only when
			// their element types hold storage.
			if readSequence(callee) && len(common.Args) == 1 {
				if yieldsStorage(value.Type(), visible) {
					return get(common.Args[0]), true
				}
				return origins{}, true
			}
			summary, found := lookup(callee)
			if !found || len(summary.ResultYields) != 1 || summary.ResultYields[0].Opaque {
				return nil, false
			}
			var bindings []ssa.Value
			if closure, ok := common.Value.(*ssa.MakeClosure); ok {
				bindings = closure.Bindings
			}
			return remap(summary.ResultYields[0].From, common.Args, bindings)
		case *ssa.ChangeType:
			// Converting a function literal to iter.Seq keeps the function.
			return yieldsOf(value.X, seen)
		case *ssa.Phi:
			out := origins{}
			for _, edge := range value.Edges {
				from, ok := yieldsOf(edge, seen)
				if !ok {
					return nil, false
				}
				out.add(from)
			}
			return out, true
		}
		return nil, false
	}
	// storedOrigins records in result.Stores that the origins in from were
	// stored into storage reached through destination. The entries are keyed
	// by the destination's parameter origins. Like a write, a store into a
	// fresh aggregate changes no parameter's storage, even when the aggregate
	// already holds parameter aliases.
	storedOrigins := func(destination ssa.Value, from origins) {
		if freshOuter(destination, map[ssa.Value]bool{}) {
			return
		}
		for target := range get(root(destination)) {
			if target < 0 {
				continue
			}
			sources := origins{}
			for source := range from {
				if source != target {
					sources[source] = true
				}
			}
			if len(sources) == 0 {
				continue
			}
			if result.Stores[target] == nil {
				result.Stores[target] = origins{}
			}
			result.Stores[target].add(sources)
		}
	}
	// stored records that value's origins entered storage reached through
	// destination.
	stored := func(destination, value ssa.Value) { storedOrigins(destination, get(value)) }

	// A value copy can detach its reference fields before publication. Its
	// origins come from the field stores dominating the load, for fields
	// whose type exposes storage.
	returned := func(value ssa.Value) origins {
		load, ok := value.(*ssa.UnOp)
		if !ok || load.Op != token.MUL {
			return get(value)
		}
		allocation, ok := load.X.(*ssa.Alloc)
		if !ok {
			return get(value)
		}
		structure, ok := load.Type().Underlying().(*types.Struct)
		if !ok {
			return get(value)
		}
		fields := make([]origins, structure.NumFields())
		for i := range fields {
			fields[i] = origins{}
		}
		var blocks []*ssa.BasicBlock
		for block := load.Block(); block != nil; block = block.Idom() {
			blocks = append(blocks, block)
		}
		slices.Reverse(blocks)
		for _, block := range fn.Blocks {
			if block.Dominates(load.Block()) {
				continue
			}
			for _, instruction := range block.Instrs {
				if store, ok := instruction.(*ssa.Store); ok && root(store.Addr) == allocation {
					return get(value)
				}
			}
		}
		for _, block := range blocks {
			for _, instruction := range block.Instrs {
				if instruction == load {
					break
				}
				store, ok := instruction.(*ssa.Store)
				if !ok || root(store.Addr) != allocation {
					continue
				}
				if store.Addr == allocation {
					for i := range fields {
						fields[i] = get(store.Val)
					}
				} else if field, ok := store.Addr.(*ssa.FieldAddr); ok && field.X == allocation {
					fields[field.Field] = get(store.Val)
				} else {
					return get(value)
				}
			}
		}
		// A field holding a read-only value handle carries nothing, as with
		// append: callers change such a handle only through its methods,
		// which are checked on their own.
		out := origins{}
		for i, field := range fields {
			if visible(structure.Field(i).Type()) {
				out.add(field)
			}
		}
		return out
	}
	for changed := true; changed; {
		changed = false
		put := func(to ssa.Value, from origins) {
			if to != nil && storage(to.Type()) {
				changed = get(to).add(from) || changed
			}
		}
		// callbacks applies the effects of closure arguments that the callee
		// may invoke, as a range-over-func iterator calls its loop body. args
		// includes the called function value when it is not a closure literal,
		// so a closure called through a variable is covered too. A closure's
		// parameters may then hold storage reachable from the other arguments,
		// or from the values those arguments were built from: a closure that
		// hands out read-only handles does not carry its captures, but code in
		// this package can still write the handles' private fields. Such
		// storage is passed with uncertain origins.
		var invoke func(closures []*ssa.MakeClosure, passed origins)
		// passedBy lists, with uncertain origins, the storage reachable from
		// args other than skip and from the values they were built from.
		passedBy := func(args []ssa.Value, skip ssa.Value) origins {
			passed := origins{}
			for _, other := range args {
				if other == skip {
					continue
				}
				if visible(other.Type()) {
					passed.add(uncertain(get(other)))
				}
				for _, source := range builtFrom(other) {
					if storage(source.Type()) {
						passed.add(uncertain(get(source)))
					}
				}
			}
			return passed
		}
		callbacks := func(args []ssa.Value) {
			for _, arg := range args {
				if closures := closuresOf(arg, map[ssa.Value]bool{}); len(closures) > 0 {
					invoke(closures, passedBy(args, arg))
				}
			}
		}
		// yieldTo applies the effects of calling a function value whose yields
		// are known, as a range-over-func loop calls its iterator with the loop
		// body. A closure argument receives only what the function value yields,
		// not everything the function value captured. A closure called through
		// the value receives the arguments as usual.
		yieldTo := func(common *ssa.CallCommon, yielded origins) {
			for _, arg := range common.Args {
				if closures := closuresOf(arg, map[ssa.Value]bool{}); len(closures) > 0 {
					invoke(closures, yielded)
				}
			}
			if closures := closuresOf(common.Value, map[ssa.Value]bool{}); len(closures) > 0 {
				invoke(closures, passedBy(common.Args, nil))
			}
		}
		// invoke applies the effects of calling closures with parameters that
		// hold the passed storage.
		invoke = func(closures []*ssa.MakeClosure, passed origins) {
			for _, closure := range closures {
				body, _ := closure.Fn.(*ssa.Function)
				if body == nil {
					continue
				}
				summary, found := lookup(body)
				if !found {
					continue
				}
				// A body that writes its parameters may write that storage.
				for index := range summary.Writes {
					if index < 0 {
						index = -index - 1
					}
					if index < len(body.Params) {
						result.Writes.add(passed)
						break
					}
				}
				// Each store the body makes into a captured variable is
				// applied to the binding: from a parameter it carries the
				// passed origins, and from another captured variable it
				// carries that binding's origins.
				for destination, sources := range summary.Stores {
					k := destination - len(body.Params)
					if k < 0 || k >= len(closure.Bindings) {
						continue
					}
					target := closure.Bindings[k]
					for source := range sources {
						possible := source < 0
						if possible {
							source = -source - 1
						}
						aliases := passed
						if j := source - len(body.Params); j >= 0 {
							if j >= len(closure.Bindings) {
								continue
							}
							aliases = get(closure.Bindings[j])
							if possible {
								aliases = uncertain(aliases)
							}
						}
						storedOrigins(target, aliases)
						put(target, aliases)
						put(root(target), aliases)
					}
				}
			}
		}
		for _, block := range fn.Blocks {
			for _, instruction := range block.Instrs {
				switch v := instruction.(type) {
				case *ssa.FieldAddr:
					put(v, get(v.X))
				case *ssa.Field:
					put(v, get(v.X))
				case *ssa.IndexAddr:
					put(v, get(v.X))
				case *ssa.Index:
					put(v, get(v.X))
				case *ssa.Lookup:
					put(v, get(v.X))
				case *ssa.Slice:
					put(v, get(v.X))
				case *ssa.SliceToArrayPointer:
					put(v, get(v.X))
				case *ssa.Range:
					changed = get(v).add(get(v.X)) || changed
				case *ssa.Next:
					changed = get(v).add(get(v.Iter)) || changed
				case *ssa.ChangeType:
					put(v, get(v.X))
				case *ssa.ChangeInterface:
					put(v, get(v.X))
				case *ssa.Convert:
					put(v, get(v.X))
				case *ssa.MakeInterface:
					put(v, get(v.X))
				case *ssa.TypeAssert:
					put(v, get(v.X))
				case *ssa.UnOp:
					if v.Op == token.MUL || v.Op == token.ARROW {
						if stored, found := storedField(v); found {
							put(v, get(stored))
							continue
						}
						put(v, get(v.X))
						put(v, get(root(v.X)))
					}
				case *ssa.Phi:
					for _, edge := range v.Edges {
						put(v, get(edge))
					}
				case *ssa.MakeClosure:
					// A closure value carries only the captured storage its body
					// can expose. Without a summary every binding is carried.
					closure, _ := v.Fn.(*ssa.Function)
					summary, found := flow{}, false
					if closure != nil {
						summary, found = lookup(closure)
					}
					for k, binding := range v.Bindings {
						if !found || captures(closure, summary, k) {
							put(v, get(binding))
						}
					}
				case *ssa.Extract:
					if slots, found := tuples[v.Tuple]; found && v.Index < len(slots) {
						put(v, slots[v.Index])
					} else {
						put(v, get(v.Tuple))
					}
				case *ssa.Store:
					write(v.Addr)
					// Assigning a slice element carries what append and copy
					// carry: nothing for a read-only value handle.
					carried := storage(v.Val.Type())
					if element, ok := v.Addr.(*ssa.IndexAddr); ok {
						if _, ok := element.X.Type().Underlying().(*types.Slice); ok {
							carried = visible(v.Val.Type())
						}
					}
					if carried {
						stored(v.Addr, v.Val)
						put(root(v.Addr), get(v.Val))
					}
				case *ssa.MapUpdate:
					write(v.Map)
					if storage(v.Value.Type()) {
						stored(v.Map, v.Value)
						put(v.Map, get(v.Value))
					}
					if storage(v.Key.Type()) {
						stored(v.Map, v.Key)
						put(v.Map, get(v.Key))
					}
				case *ssa.Send:
					write(v.Chan)
					if storage(v.X.Type()) {
						stored(v.Chan, v.X)
						put(v.Chan, get(v.X))
					}
				case *ssa.Select:
					// The result tuple is (index, recvOk, r0, r1, ...) with one
					// slot per receive state, in state order.
					received := 2
					for _, state := range v.States {
						if state.Dir == types.SendOnly {
							write(state.Chan)
							if storage(state.Send.Type()) {
								put(state.Chan, get(state.Send))
							}
							continue
						}
						if tuples[v] == nil {
							// ssa gives every select a tuple type.
							slots, _ := v.Type().(*types.Tuple)
							tuples[v] = make([]origins, slots.Len())
							for i := range tuples[v] {
								tuples[v][i] = origins{}
							}
						}
						changed = tuples[v][received].add(get(state.Chan)) || changed
						changed = tuples[v][received].add(get(root(state.Chan))) || changed
						received++
					}
				case ssa.CallInstruction:
					// go and defer statements apply the same effects but have no result.
					var call ssa.Value
					if value, ok := v.(*ssa.Call); ok {
						call = value
					}
					common := v.Common()
					args := common.Args
					if common.IsInvoke() {
						args = append([]ssa.Value{common.Value}, args...)
					}
					if builtin, ok := common.Value.(*ssa.Builtin); ok {
						switch builtin.Name() {
						case "append":
							put(call, get(args[0]))
							if s, ok := args[0].Type().Underlying().(*types.Slice); ok && visible(s.Elem()) {
								for _, arg := range args[1:] {
									put(call, get(arg))
								}
							}
							write(args[0])
						case "copy":
							write(args[0])
							if s, ok := args[0].Type().Underlying().(*types.Slice); ok && visible(s.Elem()) {
								stored(args[0], args[1])
								put(root(args[0]), get(args[1]))
							}
						case "delete", "clear", "close":
							write(args[0])
						}
						continue
					}
					if contextErr(common) || lockState(common) {
						continue
					}
					if callbackParams[common.Value] {
						for _, arg := range common.Args {
							if storage(arg.Type()) {
								result.Yields.From.add(get(arg))
							}
						}
					}
					callee := common.StaticCallee()
					if callee != nil && (byteQuery(callee) || sliceQuery(callee)) {
						continue
					}
					if callee != nil && standardClone(callee) {
						if call != nil && !detachedClone(callee, call.Type(), visible) && len(args) > 0 {
							put(call, get(args[0]))
						}
						continue
					}
					if callee != nil && readSequence(callee) {
						if call != nil && len(args) == 1 && yieldsStorage(call.Type(), visible) {
							put(call, get(args[0]))
						}
						continue
					}
					if callee != nil && collectsSequence(callee) && len(args) == 1 && fromReadSequence(args[0]) {
						put(call, get(args[0]))
						continue
					}
					// Collecting an iterator whose yields are known returns a new
					// slice of those values. Running the iterator may still write
					// what it captured, and an iterator literal still applies its
					// stores; the yield function it receives holds nothing of ours.
					if callee != nil && collectsSequence(callee) && len(args) == 1 {
						if yielded, known := yieldsOf(args[0], map[ssa.Value]bool{}); known {
							put(call, yielded)
							unknownWrite(args[0])
							invoke(closuresOf(args[0], map[ssa.Value]bool{}), origins{})
							continue
						}
					}
					if closure, ok := common.Value.(*ssa.MakeClosure); ok {
						args = append(slices.Clone(args), closure.Bindings...)
					} else if callee == nil && !common.IsInvoke() {
						// A function value carries the storage its closures captured.
						args = append(slices.Clone(args), common.Value)
					}
					if callee != nil {
						if summary, found := lookup(callee); found {
							for index := range summary.Writes {
								if index < 0 {
									index = -index - 1
									if index < len(args) {
										possibleWrite(args[index])
									}
									continue
								}
								if index < len(args) {
									write(args[index])
								}
							}
							for destination, sources := range summary.Stores {
								if destination >= len(args) {
									continue
								}
								for index := range sources {
									possible := index < 0
									if possible {
										index = -index - 1
									}
									if index >= len(args) {
										continue
									}
									aliases := get(args[index])
									if possible {
										aliases = uncertain(aliases)
									}
									stored(args[destination], args[index])
									put(args[destination], aliases)
									put(root(args[destination]), aliases)
								}
							}
							// The summary does not say what the callee passes to a
							// closure argument; that is handled as for unknown calls.
							callbacks(args)
							if call == nil {
								continue
							}
							if tuples[call] == nil {
								tuples[call] = make([]origins, len(summary.Results))
								for i := range tuples[call] {
									tuples[call][i] = origins{}
								}
							}
							for i, sources := range summary.Results {
								for index := range sources {
									possible := index < 0
									if possible {
										index = -index - 1
									}
									if index < len(args) {
										origins := get(args[index])
										if possible {
											origins = uncertain(origins)
										}
										changed = tuples[call][i].add(origins) || changed
									}
								}
								if len(summary.Results) == 1 {
									put(call, tuples[call][i])
								}
							}
							continue
						}
					}
					// Unknown and interface calls may return their reference arguments.
					// No purity or ownership is inferred from a function's name.
					for _, arg := range args {
						put(call, uncertain(get(arg)))
						unknownWrite(arg)
					}
					if callee != nil || common.IsInvoke() {
						callbacks(args)
					} else if yielded, known := yieldsOf(common.Value, map[ssa.Value]bool{}); known {
						yieldTo(common, yielded)
					} else {
						callbacks(args)
					}
				case *ssa.Return:
					for i, value := range v.Results {
						if storage(value.Type()) {
							result.Results[i].add(returned(value))
						}
						if _, function := value.Type().Underlying().(*types.Signature); function {
							if yielded, known := yieldsOf(value, map[ssa.Value]bool{}); known {
								result.ResultYields[i].From.add(yielded)
							} else {
								result.ResultYields[i].Opaque = true
							}
						}
					}
				}
			}
		}
	}
	return result
}

// builtFrom lists the values a function value was built from: the arguments
// of the call that returned it, plus the called closure's bindings, function
// value or interface receiver, or the bindings of a closure literal. It
// returns nil for any other value.
func builtFrom(value ssa.Value) []ssa.Value {
	switch value := value.(type) {
	case *ssa.Call:
		common := value.Common()
		if closure, ok := common.Value.(*ssa.MakeClosure); ok {
			return append(slices.Clone(common.Args), closure.Bindings...)
		}
		if common.StaticCallee() != nil {
			return common.Args
		}
		return append([]ssa.Value{common.Value}, common.Args...)
	case *ssa.MakeClosure:
		return value.Bindings
	}
	return nil
}

// closuresOf lists the closure literals a function value may be: the value
// itself, a literal converted to a named function type such as iter.Seq, or
// the edges of the phis it joins. Other values yield nothing.
func closuresOf(value ssa.Value, seen map[ssa.Value]bool) []*ssa.MakeClosure {
	if seen[value] {
		return nil
	}
	seen[value] = true
	switch value := value.(type) {
	case *ssa.MakeClosure:
		return []*ssa.MakeClosure{value}
	case *ssa.ChangeType:
		return closuresOf(value.X, seen)
	case *ssa.Phi:
		var out []*ssa.MakeClosure
		for _, edge := range value.Edges {
			out = append(out, closuresOf(edge, seen)...)
		}
		return out
	}
	return nil
}

// contextErr reports a call of Err on a context.Context. Its documented
// result is nil, context.Canceled or context.DeadlineExceeded: package-level
// errors that share no storage with the context. Err only reads the context.
func contextErr(common *ssa.CallCommon) bool {
	if !common.IsInvoke() || common.Method.Name() != "Err" {
		return false
	}
	named, ok := types.Unalias(common.Value.Type()).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "context" && named.Obj().Name() == "Context"
}

// lockState reports a call that locks or unlocks a sync.Mutex or sync.RWMutex.
// Such a call changes only the lock itself. It writes no other storage and
// returns no alias, so a method that locks only to read stays read-only.
// Whether locks are balanced is not checked.
func lockState(common *ssa.CallCommon) bool {
	callee := common.StaticCallee()
	if callee == nil {
		return false
	}
	obj, _ := callee.Object().(*types.Func)
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "sync" || obj.Signature().Recv() == nil {
		return false
	}
	named := namedType(obj.Signature().Recv().Type())
	if named == nil || named.Obj().Name() != "Mutex" && named.Obj().Name() != "RWMutex" {
		return false
	}
	switch obj.Name() {
	case "Lock", "Unlock", "TryLock", "RLock", "RUnlock", "TryRLock":
		return true
	}
	return false
}

// byteQuery reports a bytes package search or comparison, such as
// bytes.IndexByte or bytes.Equal. Each only reads its byte slices and returns
// an int or bool, so it writes nothing and its result aliases nothing. The
// variants taking a function, such as bytes.IndexFunc, are not included: they
// run their callback.
func byteQuery(fn *ssa.Function) bool {
	obj, _ := fn.Object().(*types.Func)
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "bytes" || obj.Signature().Recv() != nil {
		return false
	}
	switch obj.Name() {
	case "Compare", "Contains", "ContainsAny", "ContainsRune", "Count", "Equal", "EqualFold",
		"HasPrefix", "HasSuffix", "Index", "IndexAny", "IndexByte", "IndexRune",
		"LastIndex", "LastIndexAny", "LastIndexByte":
		return true
	}
	return false
}

// sliceQuery reports a slices package search or comparison of comparable or
// ordered elements, such as slices.Contains or slices.Equal. Comparing such
// elements runs no code, so the call only reads its slices and returns an int
// or bool. The variants taking a function, such as slices.ContainsFunc, are
// not included: they run their callback.
func sliceQuery(fn *ssa.Function) bool {
	obj, _ := fn.Object().(*types.Func)
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "slices" || obj.Signature().Recv() != nil {
		return false
	}
	switch obj.Name() {
	case "BinarySearch", "Compare", "Contains", "Equal", "Index", "IsSorted":
		return true
	}
	return false
}

// readSequence reports maps.Keys, maps.Values, maps.All, slices.All or
// slices.Values. Each returns an iterator over its argument. The call writes
// nothing, and calling the iterator only reads the argument.
func readSequence(fn *ssa.Function) bool {
	obj, _ := fn.Object().(*types.Func)
	if obj == nil || obj.Pkg() == nil || obj.Signature().Recv() != nil {
		return false
	}
	switch obj.Pkg().Path() {
	case "maps":
		return obj.Name() == "Keys" || obj.Name() == "Values" || obj.Name() == "All"
	case "slices":
		return obj.Name() == "All" || obj.Name() == "Values"
	}
	return false
}

// collectsSequence reports slices.Collect or slices.Sorted. Each calls its
// iterator and returns a new slice of the values it yields.
func collectsSequence(fn *ssa.Function) bool {
	obj, _ := fn.Object().(*types.Func)
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "slices" && obj.Signature().Recv() == nil &&
		(obj.Name() == "Collect" || obj.Name() == "Sorted")
}

// fromReadSequence reports an iterator returned directly by a readSequence
// call. Collecting it writes nothing.
func fromReadSequence(value ssa.Value) bool {
	call, ok := value.(*ssa.Call)
	if !ok {
		return false
	}
	callee := call.Common().StaticCallee()
	return callee != nil && readSequence(callee)
}

// yieldsStorage reports whether an iterator of type seq can yield storage. An
// iterator that yields only plain values, such as the string keys of a map,
// exposes none of the storage it reads.
func yieldsStorage(seq types.Type, visible func(types.Type) bool) bool {
	signature, ok := seq.Underlying().(*types.Signature)
	if !ok || signature.Params().Len() != 1 {
		return true
	}
	yield, ok := signature.Params().At(0).Type().Underlying().(*types.Signature)
	if !ok {
		return true
	}
	for v := range yield.Params().Variables() {
		if visible(v.Type()) {
			return true
		}
	}
	return false
}

func standardClone(fn *ssa.Function) bool {
	obj, _ := fn.Object().(*types.Func)
	if obj == nil || obj.Pkg() == nil || obj.Name() != "Clone" {
		return false
	}
	return obj.Pkg().Path() == "bytes" || obj.Pkg().Path() == "slices" || obj.Pkg().Path() == "maps"
}

// These standard-library operations allocate outer storage. A shallow clone of
// reference-bearing elements is not detached; it must still propagate aliases.
func detachedClone(fn *ssa.Function, result types.Type, visible func(types.Type) bool) bool {
	obj, _ := fn.Object().(*types.Func)
	if obj == nil || obj.Pkg() == nil || obj.Name() != "Clone" {
		return false
	}
	switch obj.Pkg().Path() {
	case "bytes":
		return true
	case "slices":
		slice, ok := result.Underlying().(*types.Slice)
		return ok && !visible(slice.Elem())
	case "maps":
		m, ok := result.Underlying().(*types.Map)
		return ok && !visible(m.Key()) && !visible(m.Elem())
	}
	return false
}
