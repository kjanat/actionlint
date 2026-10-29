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

type flow struct {
	Results []origins
	Writes  origins
	// Stores maps a destination parameter to the origins stored into its
	// storage, so a caller can carry a source's aliases into the destination.
	Stores map[int]origins
}

type functionFlow struct {
	Name    string
	Results [][]int
	Writes  []int
	// Stores pairs a destination parameter with one stored origin.
	Stores [][2]int
}

// Imported helpers carry parameter-to-result and write effects. Renaming or
// moving a helper does not change how its callers are checked.
type flowFact struct{ Functions []functionFlow }

func (*flowFact) AFact()         {}
func (*flowFact) String() string { return "function flow summaries" }

func newFlow(results int) flow {
	f := flow{Results: make([]origins, results), Writes: origins{}, Stores: map[int]origins{}}
	for i := range f.Results {
		f.Results[i] = origins{}
	}
	return f
}

func (f flow) add(other flow) bool {
	changed := f.Writes.add(other.Writes)
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
		f := newFlow(len(encoded.Results))
		for i, values := range encoded.Results {
			for _, value := range values {
				f.Results[i][value] = true
			}
		}
		for _, value := range encoded.Writes {
			f.Writes[value] = true
		}
		for _, pair := range encoded.Stores {
			if f.Stores[pair[0]] == nil {
				f.Stores[pair[0]] = origins{}
			}
			f.Stores[pair[0]][pair[1]] = true
		}
		return f, true
	}
	for changed := true; changed; {
		changed = false
		for _, fn := range functions {
			changed = flows[fn].add(analyzeFlow(fn, lookup, func(t types.Type) bool {
				return mutable(t) || storage(t) && mutatingAPI(pass, t, flows, map[types.Type]bool{}) != apiReadOnly
			})) || changed
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
		facts.Functions = append(facts.Functions, encoded)
	}
	slices.SortFunc(facts.Functions, func(a, b functionFlow) int { return strings.Compare(a.Name, b.Name) })
	pass.ExportPackageFact(&facts)
	return flows
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
			method := method.Obj().(*types.Func)
			if !method.Exported() {
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
	for i, p := range fn.Params {
		if storage(p.Type()) || (i == 0 && fn.Signature.Recv() != nil) {
			get(p)[i] = true
		}
	}
	for i, p := range fn.FreeVars {
		if storage(p.Type()) {
			get(p)[len(fn.Params)+i] = true
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
	storedField := func(load *ssa.UnOp) (ssa.Value, bool) {
		field, ok := load.X.(*ssa.FieldAddr)
		if !ok {
			return nil, false
		}
		allocation, ok := field.X.(*ssa.Alloc)
		if !ok {
			return nil, false
		}
		var blocks []*ssa.BasicBlock
		for block := load.Block(); block != nil; block = block.Idom() {
			blocks = append(blocks, block)
		}
		slices.Reverse(blocks)
		var value ssa.Value
		for _, block := range fn.Blocks {
			if block.Dominates(load.Block()) {
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
				if instruction == load {
					break
				}
				store, ok := instruction.(*ssa.Store)
				if !ok {
					continue
				}
				if store.Addr == allocation {
					value = nil
				}
				if address, ok := store.Addr.(*ssa.FieldAddr); ok && address.X == allocation && address.Field == field.Field {
					value = store.Val
				}
			}
		}
		return value, value != nil
	}
	var freshOuter func(ssa.Value, map[ssa.Value]bool) bool
	freshOuter = func(value ssa.Value, seen map[ssa.Value]bool) bool {
		if seen[value] {
			return false
		}
		seen[value] = true
		switch value := root(value).(type) {
		case *ssa.Alloc, *ssa.MakeMap, *ssa.MakeSlice:
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
	// stored records that value's origins entered storage reached through
	// destination, keyed by the destination's parameter origins.
	stored := func(destination, value ssa.Value) {
		for target := range get(root(destination)) {
			if target < 0 {
				continue
			}
			sources := origins{}
			for source := range get(value) {
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

	// A value copy can detach its reference fields before publication. Its
	// origins come from the field stores dominating the load.
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
		out := origins{}
		for i, field := range fields {
			if storage(structure.Field(i).Type()) {
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
					for _, binding := range v.Bindings {
						put(v, get(binding))
					}
				case *ssa.Extract:
					if slots, found := tuples[v.Tuple]; found && v.Index < len(slots) {
						put(v, slots[v.Index])
					} else {
						put(v, get(v.Tuple))
					}
				case *ssa.Store:
					write(v.Addr)
					if storage(v.Val.Type()) {
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
							tuples[v] = make([]origins, v.Type().(*types.Tuple).Len())
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
					callee := common.StaticCallee()
					if callee != nil && standardClone(callee) {
						if call != nil && !detachedClone(callee, call.Type(), visible) && len(args) > 0 {
							put(call, get(args[0]))
						}
						continue
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
						possibleWrite(arg)
					}
				case *ssa.Return:
					for i, value := range v.Results {
						if storage(value.Type()) {
							result.Results[i].add(returned(value))
						}
					}
				}
			}
		}
	}
	return result
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
