package configuration_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/plugins/execution/configuration"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestIntegrationDefinesItsOwnOptionsAndPaths(t *testing.T) {
	t.Parallel()
	transform, err := configuration.New(manifests.Manifest{
		ConfigSchema:           json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"search":{"type":"array","items":{"type":"string"}},"pattern":{"type":"string"},"limit":{"type":"integer","minimum":7}},"required":["limit"]}`),
		ConfigurationPaths:     []string{"/search"},
		ConfigurationTransform: &manifests.Transform{Language: manifests.LanguageJQ, Inline: `{options:{threshold:.config.limit, pattern:.config.pattern},source_prefix:"# limit="+(.config.limit|tostring)+"\n",read_access:{enabled:true,directories:.config.search}}`},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var expanded []string
	got, err := transform.Apply(t.Context(), []byte(`{"limit":7,"search":["${ROOT}/lib"],"pattern":"${ROOT}"}`), "", func(path string) (string, error) {
		expanded = append(expanded, path)
		return strings.ReplaceAll(path, "${ROOT}", "/repo"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expanded, []string{"${ROOT}/lib"}) || !reflect.DeepEqual(got.ReadAccess.Directories, []string{"/repo/lib"}) {
		t.Fatalf("expanded=%v, directories=%v", expanded, got.ReadAccess.Directories)
	}
	var options map[string]any
	if err := json.Unmarshal(got.Options, &options); err != nil {
		t.Fatal(err)
	}
	if options["threshold"] != float64(7) || options["pattern"] != "${ROOT}" || got.Prefix != "# limit=7\n" {
		t.Fatalf("options=%s, prefix=%q", got.Options, got.Prefix)
	}
	for _, invalid := range []string{`{"limit":6}`, `{"limit":"7"}`, `{"limit":7,"unknown":true}`} {
		if _, err := transform.Apply(t.Context(), []byte(invalid), "", nil); err == nil {
			t.Fatalf("invalid user setting accepted: %s", invalid)
		}
	}
}

func TestPathPointersHandleEscapedNamesAndOptionalValues(t *testing.T) {
	t.Parallel()
	transform, err := configuration.New(manifests.Manifest{ConfigurationPaths: []string{"/a~1b/~0root", "/omitted", "/nullable", "/records/0/directory"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := transform.Apply(t.Context(), []byte(`{"a/b":{"~root":"lib"},"nullable":null,"records":[{"directory":"nested"}]}`), "", func(path string) (string, error) { return "/repo/" + path, nil })
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Options) != `{"a/b":{"~root":"/repo/lib"},"nullable":null,"records":[{"directory":"/repo/nested"}]}` {
		t.Fatalf("options=%s", got.Options)
	}
}

func TestConfigurationPreservesLargeIntegers(t *testing.T) {
	t.Parallel()
	for _, program := range []string{"", `{options:.config}`} {
		manifest := manifests.Manifest{}
		if program != "" {
			manifest.ConfigurationTransform = &manifests.Transform{Language: manifests.LanguageJQ, Inline: program}
		}
		transform, err := configuration.New(manifest, nil)
		if err != nil {
			t.Fatal(err)
		}
		const input = `{"identifier":9007199254740993}`
		got, err := transform.Apply(t.Context(), []byte(input), "", nil)
		if err != nil || string(got.Options) != input {
			t.Fatalf("program=%q options=%s error=%v", program, got.Options, err)
		}
	}
}

func TestConfigurationTransformRejectsInvalidResults(t *testing.T) {
	t.Parallel()
	for _, program := range []string{`empty`, `{}, {}`, `{options:[]}`, `{options:null}`, `{options:{},typo:true}`, `{options:{},source_prefix:"# missing newline"}`} {
		t.Run(program, func(t *testing.T) {
			t.Parallel()
			transform, err := configuration.New(manifests.Manifest{ConfigurationTransform: &manifests.Transform{Language: manifests.LanguageJQ, Inline: program}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := transform.Apply(t.Context(), nil, "", nil); err == nil {
				t.Fatal("invalid transform result accepted")
			}
		})
	}
}

func TestConfigurationTransformCancelsInfinitePrograms(t *testing.T) {
	t.Parallel()
	transform, err := configuration.New(manifests.Manifest{ConfigurationTransform: &manifests.Transform{Language: manifests.LanguageJQ, Inline: `def forever: forever; forever`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := transform.Apply(ctx, nil, "", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
}
