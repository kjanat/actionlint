package config_test

import (
	"reflect"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
)

func TestEffectiveConfigFieldCoverage(t *testing.T) {
	resolved, err := repoconfig.EffectiveValues(&repoconfig.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var check func(reflect.Type, map[string]any)
	check = func(typ reflect.Type, values map[string]any) {
		t.Helper()
		keys := map[string]bool{}
		for field := range typ.Fields() {
			if !field.IsExported() {
				continue
			}
			key, options, _ := strings.Cut(field.Tag.Get("yaml"), ",")
			if key == "-" {
				continue
			}
			keys[key] = true
			value, ok := values[key]
			if !ok {
				// Omit optional unset selections to preserve inheritance.
				if strings.Contains(options, "omitempty") {
					continue
				}
				t.Errorf("%s.%s missing from resolved configuration", typ, field.Name)
				continue
			}
			if nested, ok := value.(map[string]any); ok && field.Type.Kind() == reflect.Struct {
				check(field.Type, nested)
			}
		}
		for key := range values {
			if !keys[key] {
				t.Errorf("resolved key %s not in %s", key, typ)
			}
		}
	}
	check(reflect.TypeFor[repoconfig.Config](), resolved)
}
