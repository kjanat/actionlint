package document

import (
	"fmt"
	"reflect"
	"strings"
)

// Mode selects how a configuration field combines with the preceding layer.
type Mode uint8

const (
	// Recursive merges objects member by member. Other values replace.
	Recursive Mode = iota
	// Replace replaces the whole field.
	Replace
	// AtomicMembers replaces the object's members as a unit. After an
	// inherited null, members that the patch omits are reset too.
	AtomicMembers
	// KeyedList merges list entries that share the value of Rule.Key.
	KeyedList
)

// Rule assigns merge semantics to one JSON pointer. Members lists the object
// members that AtomicMembers resets; Key names the identifying member of a
// KeyedList entry.
type Rule struct {
	Mode    Mode
	Members []string
	Key     string
}

// Plan maps JSON pointers to their merge rules. Pointers without a rule merge recursively.
type Plan map[string]Rule

// WireShape identifies the public wire fields of a private domain value.
type WireShape interface{ MergeWire() reflect.Type }

// CompilePlan derives merge rules from field tags on a configuration shape.
// Object fields are traversed; lists and maps are boundaries unless a list
// declares a key. File overrides are projected to the configuration root.
func CompilePlan(root reflect.Type) (Plan, error) {
	plan := Plan{}
	active := map[reflect.Type]bool{}
	var walk func(reflect.Type, string) error
	walk = func(typ reflect.Type, path string) error {
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || active[typ] {
			return nil
		}
		active[typ] = true
		defer delete(active, typ)
		for field := range typ.Fields() {
			if !field.IsExported() {
				continue
			}
			name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
			if name == "" || name == "-" {
				continue
			}
			child := Path(path, name)
			tag := field.Tag.Get("merge")
			switch {
			case tag == "replace":
				plan[child] = Rule{Mode: Replace}
			case tag == "atomic-members":
				ft := field.Type
				for ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				wire, ok := reflect.TypeAssert[WireShape](reflect.New(ft))
				if !ok {
					return fmt.Errorf("%s: atomic members require a wire shape", child)
				}
				shape := wire.MergeWire()
				if shape.Kind() != reflect.Struct {
					return fmt.Errorf("%s: wire shape must be a struct", child)
				}
				members := make([]string, 0, shape.NumField())
				for f := range shape.Fields() {
					if f.IsExported() {
						name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
						if name != "" && name != "-" {
							members = append(members, name)
						}
					}
				}
				plan[child] = Rule{Mode: AtomicMembers, Members: members}
			case strings.HasPrefix(tag, "key="):
				key := strings.TrimPrefix(tag, "key=")
				if key == "" || field.Type.Kind() != reflect.Slice {
					return fmt.Errorf("%s: keyed merge requires a list and key", child)
				}
				element := field.Type.Elem()
				for element.Kind() == reflect.Pointer {
					element = element.Elem()
				}
				found := false
				if element.Kind() == reflect.Struct {
					for f := range element.Fields() {
						if f.IsExported() && strings.Split(f.Tag.Get("yaml"), ",")[0] == key {
							if f.Type.Kind() != reflect.String {
								return fmt.Errorf("%s: keyed merge field %q must be a string", child, key)
							}
							found = true
							break
						}
					}
				}
				if !found {
					return fmt.Errorf("%s: keyed merge field %q is not declared", child, key)
				}
				plan[child] = Rule{Mode: KeyedList, Key: key}
			case tag != "":
				return fmt.Errorf("%s: unknown merge rule %q", child, tag)
			default:
				if err := walk(field.Type, child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root, ""); err != nil {
		return nil, err
	}
	return plan, nil
}
