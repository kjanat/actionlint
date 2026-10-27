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
}

type functionFlow struct {
	Name    string
	Results [][]int
	Writes  []int
}

// Imported helpers carry parameter-to-result and write effects. Renaming or
// moving a helper does not change how its callers are checked.
type flowFact struct{ Functions []functionFlow }

func (*flowFact) AFact()         {}
func (*flowFact) String() string { return "function flow summaries" }

func newFlow(results int) flow {
	f := flow{Results: make([]origins, results), Writes: origins{}}
	for i := range f.Results {
		f.Results[i] = origins{}
	}
	return f
}

func (f flow) add(other flow) bool {
	changed := f.Writes.add(other.Writes)
	for i := range f.Results {
		changed = f.Results[i].add(other.Results[i]) || changed
	}
	return changed
}

func functionKey(obj *types.Func) string { return obj.Origin().FullName() }

func solve(pass *analysis.Pass, functions []*ssa.Function) map[*ssa.Function]flow {
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
		return f, true
	}
	for changed := true; changed; {
		changed = false
		for _, fn := range functions {
			changed = flows[fn].add(analyzeFlow(fn, lookup, func(t types.Type) bool {
				return mutable(t) || storage(t) && mutatingAPI(pass, t, flows, map[types.Type]bool{})
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
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface, *types.TypeParam:
		return true
	case *types.Basic:
		return t.Kind() == types.UnsafePointer
	case *types.Array:
		return mutableType(t.Elem(), seen, private)
	case *types.Struct:
		for i := range t.NumFields() {
			if (private || t.Field(i).Exported()) && mutableType(t.Field(i).Type(), seen, private) {
				return true
			}
		}
	case *types.Tuple:
		for i := range t.Len() {
			if mutableType(t.At(i).Type(), seen, private) {
				return true
			}
		}
	}
	return false
}

// A private representation is not an immutable handle when its exported
// methods can mutate it. Use the same source effects as the field check.
func mutatingAPI(pass *analysis.Pass, t types.Type, flows map[*ssa.Function]flow, seen map[types.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	t = types.Unalias(t)
	if named, ok := t.(*types.Named); ok {
		methods := types.NewMethodSet(types.NewPointer(named))
		for i := range methods.Len() {
			method := methods.At(i).Obj().(*types.Func)
			if !method.Exported() {
				continue
			}
			for fn, effect := range flows {
				if obj, ok := fn.Object().(*types.Func); ok && functionKey(obj) == functionKey(method) && (effect.Writes[0] || effect.Writes[-1]) {
					return true
				}
			}
			if method.Pkg() != pass.Pkg {
				var imported flowFact
				if pass.ImportPackageFact(method.Pkg(), &imported) {
					for _, effect := range imported.Functions {
						if effect.Name == functionKey(method) && (slices.Contains(effect.Writes, 0) || slices.Contains(effect.Writes, -1)) {
							return true
						}
					}
				}
			}
		}
		return mutatingAPI(pass, named.Underlying(), flows, seen)
	}
	if structure, ok := t.(*types.Struct); ok {
		for i := range structure.NumFields() {
			if structure.Field(i).Exported() && mutatingAPI(pass, structure.Field(i).Type(), flows, seen) {
				return true
			}
		}
	}
	if array, ok := t.(*types.Array); ok {
		return mutatingAPI(pass, array.Elem(), flows, seen)
	}
	return false
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

	// A value copy can detach each reference field before publication. Read
	// the dominating stores for that load instead of confusing copied payloads
	// with aliases to the fresh aggregate itself.
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
						put(root(v.Addr), get(v.Val))
					}
				case *ssa.MapUpdate:
					write(v.Map)
					if storage(v.Value.Type()) {
						put(v.Map, get(v.Value))
					}
					if storage(v.Key.Type()) {
						put(v.Map, get(v.Key))
					}
				case *ssa.Call:
					common := v.Common()
					args := common.Args
					if common.IsInvoke() {
						args = append([]ssa.Value{common.Value}, args...)
					}
					if builtin, ok := common.Value.(*ssa.Builtin); ok {
						switch builtin.Name() {
						case "append":
							put(v, get(args[0]))
							if s, ok := v.Type().Underlying().(*types.Slice); ok && visible(s.Elem()) {
								for _, arg := range args[1:] {
									put(v, get(arg))
								}
							}
							write(args[0])
						case "copy":
							write(args[0])
							if s, ok := args[0].Type().Underlying().(*types.Slice); ok && visible(s.Elem()) {
								put(root(args[0]), get(args[1]))
							}
						case "delete", "clear":
							write(args[0])
						}
						continue
					}
					callee := common.StaticCallee()
					if callee != nil && standardClone(callee) {
						if !detachedClone(callee, v.Type(), visible) && len(args) > 0 {
							put(v, get(args[0]))
						}
						continue
					}
					if closure, ok := common.Value.(*ssa.MakeClosure); ok {
						args = append(slices.Clone(args), closure.Bindings...)
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
							if tuples[v] == nil {
								tuples[v] = make([]origins, len(summary.Results))
								for i := range tuples[v] {
									tuples[v][i] = origins{}
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
										changed = tuples[v][i].add(origins) || changed
									}
								}
								if len(summary.Results) == 1 {
									put(v, tuples[v][i])
								}
							}
							continue
						}
					}
					// Unknown and interface calls may return their reference arguments.
					// No purity or ownership is inferred from a function's name.
					for _, arg := range args {
						put(v, uncertain(get(arg)))
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
