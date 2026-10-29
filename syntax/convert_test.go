package syntax

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	wl "actionlint.kjanat.dev/v2/internal/workflow"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
)

// Parser metadata that maps decoded scalars back to YAML source is not part of
// the public tree. Every other internal field must have a public counterpart.
var omittedFields = map[string]bool{
	"String.SourceScalar":        true,
	"ExecRun.Source":             true,
	"Input.Source":               true,
	"Workflow.ExpressionSources": true,
}

// equivalent compares an internal tree with its public copy field by field.
func equivalent(t *testing.T, path string, internal, public reflect.Value) {
	t.Helper()
	switch internal.Kind() {
	case reflect.Pointer, reflect.Interface:
		if internal.IsNil() || public.IsNil() {
			if internal.IsNil() != public.IsNil() {
				t.Errorf("%s: nil mismatch", path)
			}
			return
		}
		if internal.Kind() == reflect.Pointer && internal.Pointer() == public.Pointer() {
			t.Errorf("%s: public tree shares parser storage", path)
		}
		equivalent(t, path, internal.Elem(), public.Elem())
	case reflect.Struct:
		name := internal.Type().Name()
		if public.Type().Name() != name {
			t.Errorf("%s: type %s converted to %s", path, name, public.Type().Name())
			return
		}
		for i := range internal.NumField() {
			field := internal.Type().Field(i)
			if !field.IsExported() || omittedFields[name+"."+field.Name] {
				continue
			}
			counterpart := public.FieldByName(field.Name)
			if !counterpart.IsValid() {
				t.Errorf("%s: public %s lacks field %s", path, name, field.Name)
				continue
			}
			equivalent(t, path+"."+field.Name, internal.Field(i), counterpart)
		}
	case reflect.Slice:
		if internal.IsNil() != public.IsNil() || internal.Len() != public.Len() {
			t.Errorf("%s: slice mismatch", path)
			return
		}
		for i := range internal.Len() {
			equivalent(t, path+"[]", internal.Index(i), public.Index(i))
		}
	case reflect.Map:
		if internal.IsNil() != public.IsNil() || internal.Len() != public.Len() {
			t.Errorf("%s: map mismatch", path)
			return
		}
		for _, key := range internal.MapKeys() {
			equivalent(t, path+"["+key.String()+"]", internal.MapIndex(key), public.MapIndex(key))
		}
	case reflect.Uint8:
		if internal.Uint() != public.Uint() {
			t.Errorf("%s: %d != %d", path, internal.Uint(), public.Uint())
		}
	case reflect.Int:
		if internal.Int() != public.Int() {
			t.Errorf("%s: %d != %d", path, internal.Int(), public.Int())
		}
	case reflect.Float64:
		if internal.Float() != public.Float() {
			t.Errorf("%s: %v != %v", path, internal.Float(), public.Float())
		}
	case reflect.String:
		if internal.String() != public.String() {
			t.Errorf("%s: %q != %q", path, internal.String(), public.String())
		}
	case reflect.Bool:
		if internal.Bool() != public.Bool() {
			t.Errorf("%s: %t != %t", path, internal.Bool(), public.Bool())
		}
	default:
		t.Errorf("%s: unsupported kind %s", path, internal.Kind())
	}
}

func TestParseCopiesEveryInternalField(t *testing.T) {
	var files []string
	for _, pattern := range []string{"../testdata/examples/*.yaml", "../testdata/ok/*.yaml"} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("no workflow fixtures")
	}
	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		internal, _ := workflowparser.ParseDocument(source)
		public, _ := Parse(source)
		equivalent(t, strings.TrimSuffix(filepath.Base(file), ".yaml"), reflect.ValueOf(internal), reflect.ValueOf(public))
	}
}

func TestPublicEnumsMatchParser(t *testing.T) {
	pairs := [][2]uint8{
		{uint8(CacheModeInvalid), uint8(wl.CacheModeInvalid)},
		{uint8(CacheModeNone), uint8(wl.CacheModeNone)},
		{uint8(CacheModeRead), uint8(wl.CacheModeRead)},
		{uint8(CacheModeWrite), uint8(wl.CacheModeWrite)},
		{uint8(CacheModeWriteOnly), uint8(wl.CacheModeWriteOnly)},
		{uint8(WorkflowDispatchEventInputTypeNone), uint8(wl.WorkflowDispatchEventInputTypeNone)},
		{uint8(WorkflowDispatchEventInputTypeString), uint8(wl.WorkflowDispatchEventInputTypeString)},
		{uint8(WorkflowDispatchEventInputTypeNumber), uint8(wl.WorkflowDispatchEventInputTypeNumber)},
		{uint8(WorkflowDispatchEventInputTypeBoolean), uint8(wl.WorkflowDispatchEventInputTypeBoolean)},
		{uint8(WorkflowDispatchEventInputTypeChoice), uint8(wl.WorkflowDispatchEventInputTypeChoice)},
		{uint8(WorkflowDispatchEventInputTypeEnvironment), uint8(wl.WorkflowDispatchEventInputTypeEnvironment)},
		{uint8(WorkflowCallEventInputTypeInvalid), uint8(wl.WorkflowCallEventInputTypeInvalid)},
		{uint8(WorkflowCallEventInputTypeBoolean), uint8(wl.WorkflowCallEventInputTypeBoolean)},
		{uint8(WorkflowCallEventInputTypeNumber), uint8(wl.WorkflowCallEventInputTypeNumber)},
		{uint8(WorkflowCallEventInputTypeString), uint8(wl.WorkflowCallEventInputTypeString)},
		{uint8(ExecKindAction), uint8(wl.ExecKindAction)},
		{uint8(ExecKindRun), uint8(wl.ExecKindRun)},
		{uint8(ExecKindWait), uint8(wl.ExecKindWait)},
		{uint8(ExecKindCancel), uint8(wl.ExecKindCancel)},
		{uint8(ExecKindParallel), uint8(wl.ExecKindParallel)},
		{uint8(RawYAMLValueKindObject), uint8(wl.RawYAMLValueKindObject)},
		{uint8(RawYAMLValueKindArray), uint8(wl.RawYAMLValueKindArray)},
		{uint8(RawYAMLValueKindString), uint8(wl.RawYAMLValueKindString)},
	}
	for i, pair := range pairs {
		if pair[0] != pair[1] {
			t.Errorf("enum pair %d: public %d, parser %d", i, pair[0], pair[1])
		}
	}
}
