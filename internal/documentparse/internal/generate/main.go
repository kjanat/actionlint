package main

import (
	"bytes"
	"fmt"
	"go/format"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

var structs = map[string]*types.Named{}
var interfaces = map[string]*types.Named{}
var pkg *types.Package
var clone bytes.Buffer
var views bytes.Buffer
var serial int

var semanticCollections = map[string]string{
	"String":      "stringNodes",
	"Bool":        "boolNodes",
	"Runner":      "runnerNodes",
	"DefaultsRun": "defaultsRunNodes",
	"Container":   "containerNodes",
	"Env":         "envNodes",
	"ExecAction":  "execActionNodes",
	"Strategy":    "strategyNodes",
}

func ts(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == pkg {
			return "workflowlang"
		}
		panic("unexpected model package " + p.Path())
	})
}
func gather(t types.Type) {
	switch v := t.(type) {
	case *types.Pointer:
		gather(v.Elem())
	case *types.Slice:
		gather(v.Elem())
	case *types.Map:
		if err := mapKeyError(v.Key()); err != nil {
			panic(err)
		}
		gather(v.Key())
		gather(v.Elem())
	case *types.Named:
		if v.Obj().Pkg() != pkg {
			panic("foreign named type " + v.String())
		}
		name := v.Obj().Name()
		switch body := v.Underlying().(type) {
		case *types.Struct:
			if structs[name] != nil {
				return
			}
			structs[name] = v
			for field := range body.Fields() {
				gather(field.Type())
			}
		case *types.Interface:
			if interfaces[name] != nil {
				return
			}
			interfaces[name] = v
			implementations, err := interfaceImplementations(body, pkg.Scope())
			if err != nil {
				panic(err)
			}
			for _, candidate := range implementations {
				gather(candidate)
			}
		}
	}
}
func mapKeyError(key types.Type) error {
	if !scalar(key) {
		return fmt.Errorf("unsupported mutable map key type %s: map keys must be pointer-free scalar values", types.TypeString(key, nil))
	}
	return nil
}

func interfaceImplementations(iface *types.Interface, scope *types.Scope) ([]*types.Named, error) {
	var implementations []*types.Named
	for _, name := range scope.Names() {
		object := scope.Lookup(name)
		candidate, ok := object.Type().(*types.Named)
		if !ok {
			continue
		}
		if _, ok := candidate.Underlying().(*types.Interface); ok {
			continue
		}
		if !types.Implements(types.NewPointer(candidate), iface) {
			continue
		}
		if !candidate.Obj().Exported() {
			return nil, fmt.Errorf("unsupported unexported implementation %s of closed interface", candidate.Obj().Name())
		}
		if types.Implements(candidate, iface) {
			return nil, fmt.Errorf("unsupported value-receiver implementation %s of closed interface", candidate.Obj().Name())
		}
		if _, ok := candidate.Underlying().(*types.Struct); !ok {
			return nil, fmt.Errorf("unsupported non-struct implementation %s of closed interface", candidate.Obj().Name())
		}
		implementations = append(implementations, candidate)
	}
	return implementations, nil
}

func scalar(t types.Type) bool {
	switch v := t.(type) {
	case *types.Basic:
		return v.Info()&(types.IsBoolean|types.IsNumeric|types.IsString) != 0
	case *types.Named:
		return scalar(v.Underlying())
	case *types.Struct:
		for field := range v.Fields() {
			if !scalar(field.Type()) {
				return false
			}
		}
		return true
	case *types.Array:
		return scalar(v.Elem())
	}
	return false
}
func copyInto(dst, src string, t types.Type) {
	if scalar(t) {
		fmt.Fprintf(&clone, "%s = %s\n", dst, src)
		return
	}
	switch v := t.(type) {
	case *types.Pointer:
		n, ok := v.Elem().(*types.Named)
		if !ok {
			panic(ts(t))
		}
		fmt.Fprintf(&clone, "%s = b.clone%s(%s)\n", dst, n.Obj().Name(), src)
	case *types.Named:
		if _, ok := v.Underlying().(*types.Interface); ok {
			fmt.Fprintf(&clone, "%s = b.clone%s(%s)\n", dst, v.Obj().Name(), src)
			return
		}
		panic("unhandled value " + ts(t))
	case *types.Slice:
		if n, ok := v.Elem().(*types.Named); ok && !n.Obj().Exported() {
			if !scalar(n) {
				panic("non-scalar private element")
			}
			fmt.Fprintf(&clone, "%s = slices.Clone(%s)\n", dst, src)
			return
		}
		serial++
		i := fmt.Sprintf("i%d", serial)
		fmt.Fprintf(&clone, "if %s != nil { %s = make(%s,len(%s)); for %s := range %s {\n", src, dst, ts(t), src, i, src)
		copyInto(dst+"["+i+"]", src+"["+i+"]", v.Elem())
		fmt.Fprintln(&clone, "} }")
	case *types.Map:
		serial++
		k := fmt.Sprintf("key%d", serial)
		value := fmt.Sprintf("value%d", serial)
		fmt.Fprintf(&clone, "if %s != nil { %s = make(%s,len(%s)); for %s, %s := range %s {\n", src, dst, ts(t), src, k, value, src)
		copyInto(dst+"["+k+"]", value, v.Elem())
		fmt.Fprintln(&clone, "} }")
	default:
		panic("unsupported " + ts(t))
	}
}
func viewType(t types.Type) string {
	if scalar(t) {
		return ts(t)
	}
	switch v := t.(type) {
	case *types.Pointer:
		n, ok := v.Elem().(*types.Named)
		if !ok {
			panic(ts(t))
		}
		return n.Obj().Name() + "View"
	case *types.Named:
		if _, ok := v.Underlying().(*types.Interface); ok {
			return v.Obj().Name() + "View"
		}
	}
	panic("view type " + ts(t))
}
func viewExpr(t types.Type, expr string) string {
	if scalar(t) {
		return expr
	}
	switch v := t.(type) {
	case *types.Pointer:
		n, ok := v.Elem().(*types.Named)
		if !ok {
			panic(ts(t))
		}
		return "make" + n.Obj().Name() + "View(v.owner," + expr + ")"
	case *types.Named:
		return v.Obj().Name() + "View{owner:v.owner,node:" + expr + "}"
	}
	panic(ts(t))
}
func zero(t types.Type) string {
	if !scalar(t) {
		return viewType(t) + "{}"
	}
	switch v := t.(type) {
	case *types.Basic:
		if v.Info()&types.IsString != 0 {
			return `""`
		}
		if v.Info()&types.IsBoolean != 0 {
			return "false"
		}
		return "0"
	case *types.Named:
		return zero(v.Underlying())
	case *types.Struct:
		return ts(t) + "{}"
	}
	panic(ts(t))
}
func main() {
	if len(os.Args) != 3 {
		panic("usage: root target")
	}
	ps, err := packages.Load(&packages.Config{Dir: os.Args[1], Mode: packages.NeedName | packages.NeedTypes | packages.NeedImports | packages.NeedDeps}, "./internal/workflow")
	if err != nil || len(ps) != 1 || len(ps[0].Errors) != 0 {
		panic(fmt.Sprint(err, ps))
	}
	pkg = ps[0].Types
	gather(pkg.Scope().Lookup("Workflow").Type())
	var names []string
	for name, n := range structs {
		if n.Obj().Exported() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for name := range semanticCollections {
		if structs[name] == nil {
			panic("semantic node type outside Workflow closure: " + name)
		}
	}
	fmt.Fprintln(&clone, "// Code generated by internal/documentparse/internal/generate; DO NOT EDIT.\n\npackage frozen\n\nimport (\"slices\"; workflowlang \"actionlint.kjanat.dev/v2/internal/workflow\")")
	fmt.Fprintln(&views, "// Code generated by internal/documentparse/internal/generate; DO NOT EDIT.\n\npackage frozen\n\nimport (\"iter\"; workflowlang \"actionlint.kjanat.dev/v2/internal/workflow\")")
	for _, name := range names {
		n := structs[name]
		body, ok := n.Underlying().(*types.Struct)
		if !ok {
			panic(name)
		}
		fmt.Fprintf(&clone, "func (b *builder) clone%s(in *workflowlang.%s) *workflowlang.%s { if in == nil {return nil}; out := new(workflowlang.%s)\n", name, name, name, name)
		if name == "Step" {
			fmt.Fprintln(&clone, "b.stepCursors[in] = len(b.steps); b.steps = append(b.steps, out)")
		}
		if name == "Job" {
			fmt.Fprintln(&clone, "b.jobCursors[in] = len(b.jobs); b.jobs = append(b.jobs, out)")
		}
		if collection, ok := semanticCollections[name]; ok {
			fmt.Fprintf(&clone, "b.%s = append(b.%s, out)\n", collection, collection)
		}
		for field := range body.Fields() {
			if !field.Exported() {
				panic("private model field " + name + "." + field.Name())
			}
			copyInto("out."+field.Name(), "in."+field.Name(), field.Type())
		}
		fmt.Fprintln(&clone, "return out }")
		if name == "Step" || name == "Job" {
			fmt.Fprintf(&views, "type %sView struct {owner *Bound; index int}\nfunc make%sView(owner *Bound,node *workflowlang.%s) %sView {if owner==nil||node==nil{return %sView{}};index,ok:=owner.record.%sIndices[node];if !ok{return %sView{}};return %sView{owner:owner,index:index}}\nfunc(v %sView) data()*workflowlang.%s {if v.owner==nil||v.index<0||v.index>=len(v.owner.record.%ss){return nil};return v.owner.record.%ss[v.index]}\n", name, name, name, name, name, strings.ToLower(name), name, name, name, name, strings.ToLower(name), strings.ToLower(name))
		} else {
			fmt.Fprintf(&views, "type %sView struct {owner *Bound; node *workflowlang.%s; _ [0]func()}\nfunc make%sView(owner *Bound,node *workflowlang.%s) %sView {if owner==nil||node==nil{return %sView{}};return %sView{owner:owner,node:node}}\nfunc(v %sView)data()*workflowlang.%s{return v.node}\n", name, name, name, name, name, name, name, name, name)
		}
		fmt.Fprintf(&views, "func(v %sView)Present()bool{return v.data()!=nil}\n", name)
		for f := range body.Fields() {
			fn := f.Name()
			t := f.Type()
			expr := "node." + fn
			if name == "ScriptSource" && fn == "Lines" {
				continue
			}
			if ptr, ok := t.(*types.Pointer); ok {
				if nn, ok := ptr.Elem().(*types.Named); ok && nn.Obj().Name() == "Pos" {
					fmt.Fprintf(&views, "func(v %sView)%s()(workflowlang.Pos,bool){node:=v.data();if node==nil||%s==nil{return workflowlang.Pos{},false};return *%s,true}\n", name, fn, expr, expr)
					continue
				}
			}
			switch field := t.(type) {
			case *types.Slice:
				et := viewType(field.Elem())
				fmt.Fprintf(&views, "func(v %sView)%s()iter.Seq[%s]{return func(yield func(%s)bool){node:=v.data();if node==nil{return};for _,element:=range %s{if !yield(%s){return}}}}\nfunc(v %sView)%sPresent()bool{node:=v.data();return node!=nil&&%s!=nil}\n", name, fn, et, et, expr, viewExpr(field.Elem(), "element"), name, fn, expr)
			case *types.Map:
				kt, vt := ts(field.Key()), viewType(field.Elem())
				fmt.Fprintf(&views, "func(v %sView)%s()iter.Seq2[%s,%s]{return func(yield func(%s,%s)bool){node:=v.data();if node==nil{return};for key,element:=range %s{if !yield(key,%s){return}}}}\nfunc(v %sView)%sAt(key %s)(%s,bool){node:=v.data();if node==nil{return %s,false};element,ok:=%s[key];if !ok{return %s,false};return %s,true}\nfunc(v %sView)%sPresent()bool{node:=v.data();return node!=nil&&%s!=nil}\n", name, fn, kt, vt, kt, vt, expr, viewExpr(field.Elem(), "element"), name, fn, kt, vt, zero(field.Elem()), expr, zero(field.Elem()), viewExpr(field.Elem(), "element"), name, fn, expr)
			default:
				fmt.Fprintf(&views, "func(v %sView)%s()%s{node:=v.data();if node==nil{return %s};return %s}\n", name, fn, viewType(t), zero(t), viewExpr(t, expr))
			}
		}
	}
	var ifNames []string
	for name := range interfaces {
		ifNames = append(ifNames, name)
	}
	sort.Strings(ifNames)
	for _, name := range ifNames {
		n := interfaces[name]
		iface, ok := n.Underlying().(*types.Interface)
		if !ok {
			panic(name)
		}
		fmt.Fprintf(&clone, "func(b *builder)clone%s(in workflowlang.%s)workflowlang.%s{switch value:=in.(type){case nil:return nil\n", name, name, name)
		fmt.Fprintf(&views, "type %sView struct {owner *Bound;node workflowlang.%s; _ [0]func()}\nfunc(v %sView)Present()bool{return v.node!=nil}\n", name, name, name)
		for _, candidate := range names {
			cn := structs[candidate]
			if !types.Implements(types.NewPointer(cn), iface) {
				continue
			}
			fmt.Fprintf(&clone, "case *workflowlang.%s:return b.clone%s(value)\n", candidate, candidate)
			method := candidate
			if name == "Exec" {
				method = strings.TrimPrefix(candidate, "Exec")
			}
			fmt.Fprintf(&views, "func(v %sView)%s()(%sView,bool){node,ok:=v.node.(*workflowlang.%s);return make%sView(v.owner,node),ok}\n", name, method, candidate, candidate, candidate)
		}
		fmt.Fprintf(&clone, "default:panic(\"unpublished %s implementation\")}}\n", name)
	}
	var aliases bytes.Buffer
	fmt.Fprintln(&aliases, "// Code generated by internal/documentparse/internal/generate; DO NOT EDIT.\n\npackage documentparse\n\nimport \"actionlint.kjanat.dev/v2/internal/documentparse/internal/frozen\"")
	for _, name := range append(names, ifNames...) {
		fmt.Fprintf(&aliases, "type %sView = frozen.%sView\n", name, name)
	}
	for _, alias := range []struct{ name, target string }{
		{"RunView", "ExecRunView"}, {"ActionView", "ExecActionView"}, {"ParallelView", "ExecParallelView"},
		{"SourceView", "ScriptSourceView"}, {"ActionBodyView", "ActionBodyView"}, {"DeclarationOrigin", "Origin"}, {"ScalarLine", "ScalarLine"},
	} {
		fmt.Fprintf(&aliases, "type %s = frozen.%s\n", alias.name, alias.target)
	}
	for file, data := range map[string][]byte{"clone.gen.go": clone.Bytes(), "views.gen.go": views.Bytes(), "../../owned_views.gen.go": aliases.Bytes()} {
		formatted, err := format.Source(data)
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(os.Args[2], file), formatted, 0644); err != nil {
			panic(err)
		}
	}
	fmt.Printf("generated %d model structs, %d closed interfaces\n", len(names), len(interfaces))
}
