package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const repository = "actions/runner"
const schemaPath = "src/Runner.Worker/action_yaml.json"

type definition struct {
	Context []string `json:"context"`
	OneOf   []string `json:"one-of"`
	Mapping *struct {
		Properties map[string]json.RawMessage `json:"properties"`
		LooseValue string                     `json:"loose-value-type"`
	} `json:"mapping"`
	Sequence *struct {
		Item string `json:"item-type"`
	} `json:"sequence"`
}

type limits struct{ Min, Max int }

type availability struct {
	Contexts  []string
	Functions map[string]limits
}

type tables struct {
	Availability map[string]availability
	Keys         map[string][]string
	Functions    []string
}

var functionPattern = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*)\(([0-9]+),([0-9]+|MAX)\)$`)
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func readSchema(data []byte) (map[string]definition, error) {
	var schema struct {
		Definitions map[string]definition `json:"definitions"`
	}
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &schema); err != nil {
		return nil, err
	}
	if _, ok := schema.Definitions["action-root"]; !ok {
		return nil, errors.New("schema has no action-root definition")
	}
	return schema.Definitions, nil
}

func collect(defs map[string]definition) (*tables, error) {
	t := &tables{Availability: map[string]availability{}, Keys: map[string][]string{}}
	var walk func(string, string, []string, []string) error
	walk = func(name, path string, inherited, stack []string) error {
		if slices.Contains(stack, name) {
			return fmt.Errorf("recursive schema definition %q at %q", name, path)
		}
		d, ok := defs[name]
		if !ok && !slices.Contains([]string{"any", "string", "boolean", "number", "null", "sequence", "mapping"}, name) {
			return fmt.Errorf("unknown schema definition %q at %q", name, path)
		}
		stack = append(slices.Clone(stack), name)
		context := slices.Concat(inherited, d.Context)
		a := availability{Functions: map[string]limits{}}
		for _, entry := range context {
			if m := functionPattern.FindStringSubmatch(entry); m != nil {
				minArgs, err := strconv.Atoi(m[2])
				if err != nil {
					return err
				}
				maxArgs := -1
				if m[3] != "MAX" {
					maxArgs, err = strconv.Atoi(m[3])
					if err != nil || maxArgs < minArgs {
						return fmt.Errorf("invalid function limits %q", entry)
					}
				}
				funcName := strings.ToLower(m[1])
				a.Functions[funcName] = limits{minArgs, maxArgs}
				t.Functions = append(t.Functions, funcName)
			} else if entry == "" || strings.ContainsAny(entry, "(), ") {
				return fmt.Errorf("unsupported context entry %q", entry)
			} else {
				a.Contexts = append(a.Contexts, strings.ToLower(entry))
			}
		}
		slices.Sort(a.Contexts)
		a.Contexts = slices.Compact(a.Contexts)
		if len(d.OneOf) > 0 {
			for _, child := range d.OneOf {
				if err := walk(child, path, context, stack); err != nil {
					return err
				}
			}
			return nil
		}
		if len(context) > 0 {
			if prev, ok := t.Availability[path]; ok && (!slices.Equal(prev.Contexts, a.Contexts) || !maps.Equal(prev.Functions, a.Functions)) {
				return fmt.Errorf("conflicting context definitions at %q", path)
			}
			t.Availability[path] = a
		}
		if d.Mapping != nil {
			for _, key := range slices.Sorted(maps.Keys(d.Mapping.Properties)) {
				raw := d.Mapping.Properties[key]
				var child string
				if err := json.Unmarshal(raw, &child); err != nil {
					var prop struct{ Type string }
					if err := json.Unmarshal(raw, &prop); err != nil || prop.Type == "" {
						return fmt.Errorf("invalid property %q in %q", key, name)
					}
					child = prop.Type
				}
				if err := walk(child, strings.TrimPrefix(path+"."+key, "."), context, stack); err != nil {
					return err
				}
			}
			t.Keys[name] = slices.Sorted(maps.Keys(d.Mapping.Properties))
			if d.Mapping.LooseValue != "" {
				return walk(d.Mapping.LooseValue, path+".*", context, stack)
			}
		}
		if d.Sequence != nil {
			return walk(d.Sequence.Item, path+".*", context, stack)
		}
		return nil
	}
	if err := walk("action-root", "", nil, nil); err != nil {
		return nil, err
	}
	for _, path := range []string{
		"inputs.*.default", "runs.steps.*.run", "runs.steps.*.if", "runs.steps.*.name",
		"runs.steps.*.shell", "runs.steps.*.continue-on-error", "runs.steps.*.working-directory",
		"runs.steps.*.env", "runs.steps.*.env.*", "runs.steps.*.with", "runs.steps.*.with.*",
	} {
		if _, ok := t.Availability[path]; !ok {
			return nil, fmt.Errorf("schema lost required expression field %q", path)
		}
	}
	for _, name := range []string{"run-step", "uses-step"} {
		if len(t.Keys[name]) == 0 || defs[name].Mapping.LooseValue != "" {
			return nil, fmt.Errorf("schema lost closed step mapping %q", name)
		}
	}
	slices.Sort(t.Functions)
	t.Functions = slices.Compact(t.Functions)
	return t, nil
}

func generate(data []byte, revision string) ([]byte, error) {
	if !revisionPattern.MatchString(revision) {
		return nil, errors.New("invalid runner schema revision")
	}
	defs, err := readSchema(data)
	if err != nil {
		return nil, err
	}
	t, err := collect(defs)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	fmt.Fprintln(&out, "// Code generated by actionlint/scripts/generate-action-metadata. DO NOT EDIT.")
	fmt.Fprintf(&out, "// Source: https://github.com/%s/blob/%s/%s\n\npackage actionlint\n\n", repository, revision, schemaPath)
	fmt.Fprintln(&out, "var actionMetadataAvailability = map[string]actionExpressionAvailability{")
	for _, path := range slices.Sorted(maps.Keys(t.Availability)) {
		a := t.Availability[path]
		fmt.Fprintf(&out, "%q: {\ncontexts: %#v,\nfunctions: map[string]actionFunctionLimits{\n", path, a.Contexts)
		for _, name := range slices.Sorted(maps.Keys(a.Functions)) {
			v := a.Functions[name]
			fmt.Fprintf(&out, "%q: {%d, %d},\n", name, v.Min, v.Max)
		}
		fmt.Fprintln(&out, "},\n},")
	}
	fmt.Fprintln(&out, "}")
	fmt.Fprintln(&out, "var actionMetadataKeys = map[string][]string{")
	for _, name := range slices.Sorted(maps.Keys(t.Keys)) {
		if name == "run-step" || name == "uses-step" {
			fmt.Fprintf(&out, "%q: {%s},\n", name, quoted(t.Keys[name]))
		}
	}
	fmt.Fprintln(&out, "}")
	fmt.Fprintf(&out, "var actionMetadataSpecialFunctions = %#v\n", t.Functions)
	return format.Source(out.Bytes())
}

func quoted(values []string) string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = strconv.Quote(value)
	}
	return strings.Join(out, ", ")
}

func download(ctx context.Context, client *http.Client, url, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func refresh(ctx context.Context, client *http.Client, token, output string) error {
	url := "https://api.github.com/repos/" + repository + "/commits?path=" + schemaPath + "&per_page=1"
	data, err := download(ctx, client, url, token)
	if err != nil {
		return err
	}
	var commits []struct{ SHA string }
	if err := json.Unmarshal(data, &commits); err != nil {
		return fmt.Errorf("read runner revision: %w", err)
	}
	if len(commits) == 0 || !revisionPattern.MatchString(commits[0].SHA) {
		return errors.New("GitHub returned no valid runner schema revision")
	}
	revision := commits[0].SHA
	url = fmt.Sprintf("https://cdn.jsdelivr.net/gh/%s@%s/%s", repository, revision, schemaPath)
	data, err = download(ctx, client, url, "")
	if err != nil {
		return err
	}
	generated, err := generate(data, revision)
	if err != nil {
		return err
	}
	return os.WriteFile(output, generated, 0o644)
}

func run(args []string) error {
	f := flag.NewFlagSet("generate-action-metadata", flag.ContinueOnError)
	output := f.String("output", "action_metadata_availability.go", "Generated Go file")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if !filepath.IsLocal(*output) {
		return fmt.Errorf("output file path must be relative to the current directory: %q", *output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	return refresh(ctx, http.DefaultClient, token, *output)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
