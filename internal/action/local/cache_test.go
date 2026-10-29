package local

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func testGetWantedActionMetadata() *actionmeta.Metadata {
	want := &actionmeta.Metadata{
		Name:        "My action",
		Description: "my action",
		Inputs: actionmeta.Inputs{
			"name":     {Name: "name"},
			"message":  {Name: "message", Required: true},
			"addition": {Name: "addition"},
		},
		Outputs: actionmeta.Outputs{
			"user_id": {Name: "user_id"},
		},
		Runs: actionmeta.Runs{
			Using: "node20",
			Main:  "index.js",
		},
		InputDefaults: []*actionmeta.KeyValue{
			{Name: "name", Value: actionmeta.ExprString{Value: "anonymous", Line: 8, Column: 14}},
		},
	}
	return want
}

func testDiffActionMetadata(t *testing.T, want, have *actionmeta.Metadata, opts ...cmp.Option) {
	t.Helper()
	opts = append(opts, cmpopts.IgnoreUnexported(actionmeta.Metadata{}), cmpopts.IgnoreFields(actionmeta.Metadata{}, "SourceMapped", "Directory", "Filename", "Source", "Root"))
	if diff := cmp.Diff(want, have, opts...); diff != "" {
		t.Fatal(diff)
	}
}

func testCheckActionMetadataPath(t *testing.T, dir string, m *actionmeta.Metadata) {
	t.Helper()

	var want string
	d := filepath.Join("../../../testdata", "action_metadata", dir)
	for _, f := range []string{"action.yml", "action.yaml"} {
		p := filepath.Join(d, f)
		if _, err := os.Stat(p); err == nil {
			want = p
			break
		}
	}
	if want == "" {
		panic("metadata file doesn't exist in " + d)
	}

	if have := m.Path(); have != want {
		t.Errorf("action metadata file path for %q is actually %q but wanted %q", dir, have, want)
	}

	want = filepath.Dir(want)
	if have := m.Dir(); have != want {
		t.Errorf("action directory path for %q is actually %q but wanted %q", dir, have, want)
	}
}

func testCheckCachedFlag(t *testing.T, want, have bool) {
	t.Helper()
	if want != have {
		msg := "metadata should be cached but actually it is not cached"
		if !want {
			msg = "metadata should not be cached but actually it is cached"
		}
		t.Error(msg)
	}
}

// Normal cases

func TestLocalActionsFindMetadataOK(t *testing.T) {
	testdir := filepath.Join("../../../testdata", "action_metadata")
	root := testdir
	c := newCapturedActionCache(t, root, nil)

	want := testGetWantedActionMetadata()

	wantEmpty := testGetWantedActionMetadata()
	wantEmpty.Inputs = nil
	wantEmpty.Outputs = nil
	wantEmpty.InputDefaults = nil

	wantUpper := testGetWantedActionMetadata()
	for _, i := range wantUpper.Inputs {
		i.Name = strings.ToUpper(i.Name)
	}
	for _, o := range wantUpper.Outputs {
		o.Name = strings.ToUpper(o.Name)
	}
	for _, d := range wantUpper.InputDefaults {
		d.Name = strings.ToUpper(d.Name)
	}

	wantBranding := testGetWantedActionMetadata()
	wantBranding.Branding.Icon = "edit"
	wantBranding.Branding.Color = "white"
	// The "branding" section pushes the inputs (and the "default" value node) down the file.
	wantBranding.InputDefaults = []*actionmeta.KeyValue{
		{Name: "name", Value: actionmeta.ExprString{Value: "anonymous", Line: 11, Column: 14}},
	}

	wantNode24 := testGetWantedActionMetadata()
	wantNode24.Runs.Using = "node24"

	wantDeprecated := testGetWantedActionMetadata()
	for _, i := range wantDeprecated.Inputs {
		i.Deprecated = true
		i.DeprecationMessage = "This is deprecated"
	}

	tests := []struct {
		spec string
		want *actionmeta.Metadata
		cmp  []cmp.Option
	}{
		{
			spec: "./action-yml",
			want: want,
		},
		{
			spec: "./action-yaml",
			want: want,
		},
		{
			spec: "./empty",
			want: wantEmpty,
		},
		{
			spec: "./uppercase",
			want: wantUpper,
		},
		{
			spec: "./docker",
			want: &actionmeta.Metadata{
				Name:        "My action",
				Description: "my action",
				Runs: actionmeta.Runs{
					Using: "docker",
					Image: "Dockerfile",
				},
			},
		},
		{
			spec: "./composite",
			want: &actionmeta.Metadata{
				Name:        "My action",
				Description: "my action",
				Runs: actionmeta.Runs{
					Using: "composite",
				},
			},
			cmp: []cmp.Option{
				cmpopts.IgnoreFields(actionmeta.Runs{}, "Steps"),
			},
		},
		{
			spec: "./branding",
			want: wantBranding,
		},
		{
			spec: "./node24",
			want: wantNode24,
		},
		{
			spec: "./deprecated",
			want: wantDeprecated,
		},
	}

	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			// read metadata repeatedly (should be cached)
			for i := range 3 {
				have, cached, err := c.FindMetadata(tc.spec)
				if err != nil {
					t.Fatal(i, err)
				}
				if have == nil {
					t.Fatal(i, "metadata is nil")
				}
				testCheckCachedFlag(t, cached, i > 0)
				testDiffActionMetadata(t, tc.want, have, tc.cmp...)
				testCheckActionMetadataPath(t, tc.spec, have)
			}
		})
	}
}

func TestLocalActionsFindConcurrently(t *testing.T) {
	n := 10
	root := filepath.Join("../../../testdata", "action_metadata")
	c := newCapturedActionCache(t, root, nil)
	ret := make(chan *actionmeta.Metadata)
	err := make(chan error)

	for range n {
		go func() {
			m, _, e := c.FindMetadata("./action-yml")
			if e != nil {
				err <- e
				return
			}
			ret <- m
		}()
	}

	ms := []*actionmeta.Metadata{}
	errs := []error{}
	for range n {
		select {
		case m := <-ret:
			ms = append(ms, m)
		case e := <-err:
			errs = append(errs, e)
		}
	}

	if len(errs) != 0 {
		t.Fatal("some error occurred:", errs)
	}

	want := testGetWantedActionMetadata()
	for _, have := range ms {
		testDiffActionMetadata(t, want, have)
	}

	_, cached, _ := c.FindMetadata("./action-yml")
	testCheckCachedFlag(t, true, cached)
}

func TestLocalActionsParsingSkipped(t *testing.T) {
	tests := []struct {
		what string
		root string
		spec string
	}{
		{
			what: "project is nil",
			root: "",
			spec: "./action",
		},
		{
			what: "not a local action",
			root: "",
			spec: "actions/checkout@v4",
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			c := newCapturedActionCache(t, tc.root, nil)
			m, cached, err := c.FindMetadata(tc.spec)
			if err != nil {
				t.Fatal(tc.spec, "error occurred:", err)
			}
			if m != nil {
				t.Fatal(tc.spec, "metadata was parsed", m)
			}
			testCheckCachedFlag(t, false, cached)
		})
	}
}

func TestLocalActionsMissingMetadata(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "empty"), 0750); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []string{"./missing", "./empty", "./ }", "$/missing"} {
		c := newCapturedActionCache(t, root, nil)
		for attempt := range 2 {
			meta, cached, err := c.FindMetadata(spec)
			if meta != nil || err == nil || !strings.Contains(err.Error(), "has no action.yml, action.yaml") {
				t.Fatalf("%s: meta=%v err=%v", spec, meta, err)
			}
			if cached != (attempt > 0) {
				t.Fatalf("%s: cached=%v on attempt %d", spec, cached, attempt)
			}
		}
	}
}

func TestLocalActionsCaseScopedCache(t *testing.T) {
	root := t.TempDir()
	file := writeFixture(t, root, "local/Nested/action.yml", `name: local
description: test
runs: {using: node24, main: index.js}
`)
	base := newCapturedActionCache(t, root, nil)
	folded := &Resolver{base: base, caseInsensitive: true}
	exact := &Resolver{base: base}
	for _, spec := range []string{"./LOCAL/nested", "$/LOCAL/nested", "./local/Nested"} {
		metadata, _, err := folded.FindMetadata(spec)
		if err != nil || metadata == nil {
			t.Fatalf("%s: metadata=%v, error=%v", spec, metadata, err)
		}
		if metadata.Path() != file {
			t.Fatalf("%s: path=%q, want %q", spec, metadata.Path(), file)
		}
	}
	if _, cached := base.readCache("./LOCAL/nested"); cached {
		t.Fatal("case-insensitive alias leaked into shared cache")
	}
	_, statErr := os.Stat(filepath.Join(root, "LOCAL", "nested", "action.yml"))
	metadata, _, err := exact.FindMetadata("./LOCAL/nested")
	if (err == nil) != (statErr == nil) || (metadata != nil) != (statErr == nil) {
		t.Fatalf("exact view changed host lookup behavior: metadata=%v, error=%v", metadata, err)
	}
}

func TestLocalActionsCaseCollision(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "local/action.yml", "name: lower\n")
	if _, err := os.Stat(filepath.Join(root, "LOCAL")); err == nil {
		t.Skip("case-sensitive filesystem required for colliding directories")
	}
	writeFixture(t, root, "LOCAL/action.yml", "name: upper\n")
	base := newCapturedActionCache(t, root, nil)
	view := &Resolver{base: base, caseInsensitive: true}
	for _, spec := range []string{"./local", "./LOCAL", "./Local", "$/LOCAL"} {
		metadata, _, err := view.FindMetadata(spec)
		if err == nil || metadata != nil {
			t.Fatalf("ambiguous %s: metadata=%v, error=%v", spec, metadata, err)
		}
	}
}

func TestLocalActionsIgnoreRemoteActions(t *testing.T) {
	root := filepath.Join("../../../testdata", "action_metadata")
	c := newCapturedActionCache(t, root, nil)
	for _, spec := range []string{"actions/checkout@v2", "docker://example.com/foo/bar"} {
		m, cached, err := c.FindMetadata(spec)
		if err != nil {
			t.Fatal(spec, "error occurred:", err)
		}
		if m != nil {
			t.Fatal(spec, "metadata was parsed", m)
		}
		testCheckCachedFlag(t, false, cached)
	}
}

func TestLocalActionsLogCacheHit(t *testing.T) {
	dbg := &bytes.Buffer{}
	testdir := filepath.Join("../../../testdata", "action_metadata")
	root := testdir
	c := newCapturedActionCache(t, root, dbg)

	want := testGetWantedActionMetadata()
	for range 2 {
		have, _, err := c.FindMetadata("./action-yml")
		if err != nil {
			t.Fatal(err)
		}
		testDiffActionMetadata(t, want, have)
	}

	logs := strings.Split(strings.TrimSpace(dbg.String()), "\n")
	if len(logs) != 2 {
		t.Fatalf("2 logs were expected but got %d logs: %#v", len(logs), logs)
	}
	dir := filepath.Join(testdir, "action-yml")
	if !strings.Contains(logs[0], "New metadata parsed from action "+dir) {
		t.Fatalf("first log should be 'new metadata' but got %q", logs[0])
	}
	if !strings.Contains(logs[1], "Cache hit for ./action-yml") {
		t.Fatalf("second log should be 'cache hit' but got %q", logs[1])
	}
}

func TestLocalActionsNullCache(t *testing.T) {
	c := newCapturedActionCache(t, "", io.Discard)
	m, cached, err := c.FindMetadata("./path/to/action.yaml")
	if m != nil {
		t.Error("metadata should not be found:", m)
	}
	if err != nil {
		t.Error(err)
	}
	testCheckCachedFlag(t, false, cached)
}

// Error cases

func TestLocalActionsBrokenMetadata(t *testing.T) {
	tests := []struct {
		spec string
		want string
	}{
		{
			spec: "./broken",
			want: "could not parse action metadata",
		},
	}

	root := filepath.Join("../../../testdata", "action_metadata")
	c := newCapturedActionCache(t, root, nil)

	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			m, cached, err := c.FindMetadata(tc.spec)
			if err == nil {
				t.Fatal("error was not returned", m)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error message %q to contain %q", err, tc.want)
			}
			testCheckCachedFlag(t, false, cached)

			// Second try does not return error, but metadata is also nil not to show the same error from
			// multiple rules.
			m, cached, err = c.FindMetadata(tc.spec)
			if err != nil {
				t.Fatal("error was returned at second try", err)
			}
			if m != nil {
				t.Fatal("metadata was not nil even if it does not exist", m)
			}
			testCheckCachedFlag(t, true, cached)
		})
	}
}

func TestLocalActionsDuplicateInputsOutputs(t *testing.T) {
	root := filepath.Join("../../../testdata", "action_metadata")
	c := newCapturedActionCache(t, root, nil)

	for _, tc := range []struct {
		spec string
		want string
	}{
		{
			spec: "./input-duplicate",
			want: "input \"FOO\" is duplicated",
		},
		{
			spec: "./output-duplicate",
			want: "output \"FOO\" is duplicated",
		},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			m, cached, err := c.FindMetadata(tc.spec)
			if err == nil {
				t.Fatal("error was not returned", m)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("error %q was expected to include %q", msg, tc.want)
			}
			testCheckCachedFlag(t, false, cached)
		})
	}
}

func TestLocalActionsConcurrentFailures(t *testing.T) {
	n := 10
	root := filepath.Join("../../../testdata", "action_metadata")
	c := newCapturedActionCache(t, root, nil)
	errC := make(chan error)

	for range n {
		go func() {
			_, _, err := c.FindMetadata("./broken")
			errC <- err
		}()
	}

	errs := []error{}
	for range n {
		errs = append(errs, <-errC)
	}

	// At least once error was reported
	var err error
	for _, e := range errs {
		if e != nil {
			err = e
			break
		}
	}

	if err == nil {
		t.Fatal("error did not occur", err)
	}
	if !strings.Contains(err.Error(), "could not parse action metadata") {
		t.Fatal("unexpected error:", err)
	}
}

func TestLocalActionsConcurrentMultipleMetadataAndFailures(t *testing.T) {
	root := filepath.Join("../../../testdata", "action_metadata")
	c := newCapturedActionCache(t, root, nil)

	inputs := []string{
		"./action-yml",
		"./action-yaml",
		"./action-yml",
		"./broken",
		"./action-yaml",
		"./action-yaml",
		"./broken",
		"./action-yml",
		"./broken",
		"./action-yaml",
	}

	reqC := make(chan string)
	retC := make(chan *actionmeta.Metadata)
	errC := make(chan error)
	done := make(chan struct{})

	for range 3 {
		go func() {
			for {
				select {
				case spec := <-reqC:
					m, _, err := c.FindMetadata(spec)
					if m == nil {
						errC <- err
						break
					}
					retC <- m
				case <-done:
					return
				}
			}
		}()
	}

	go func() {
		for _, in := range inputs {
			select {
			case reqC <- in:
			case <-done:
				return
			}
		}
	}()

	ret := []*actionmeta.Metadata{}
	errs := []error{}
	for range inputs {
		select {
		case m := <-retC:
			ret = append(ret, m)
		case err := <-errC:
			errs = append(errs, err)
		}
	}
	close(done)

	numErrs := 0
	for _, in := range inputs {
		if in == "./broken" {
			numErrs++
		}
	}
	numRet := len(inputs) - numErrs

	if len(errs) != numErrs {
		t.Fatalf("wanted %d errors but got %d: %v", numErrs, len(errs), errs)
	}
	if len(ret) != numRet {
		t.Fatalf("wanted %d errors but got %d: %v", numRet, len(ret), ret)
	}

	var err error
	for _, e := range errs {
		if e != nil {
			err = e
			break
		}
	}

	if err == nil {
		t.Fatal("error did not occur", err)
	}
	if !strings.Contains(err.Error(), "could not parse action metadata") {
		t.Fatal("unexpected error:", err)
	}

	want := testGetWantedActionMetadata()
	for _, have := range ret {
		testDiffActionMetadata(t, want, have)
	}
}

func TestLocalActionsCacheFactory(t *testing.T) {
	f := NewFactory(io.Discard)
	p1 := "path/to/project1"
	c1 := f.GetCache(t.Context(), p1)

	p2 := "path/to/project2"
	c2 := f.GetCache(t.Context(), p2)
	if c1 == c2 {
		t.Errorf("different cache was not created: %v", c1)
	}

	c3 := f.GetCache(t.Context(), p1)
	if c1 != c3 {
		t.Errorf("same cache is not returned for the same project: %v vs %v", c1, c3)
	}

	c4 := f.GetCache(t.Context(), "")
	if c4.root != "" {
		t.Errorf("null cache must be returned if given project is nil: %v", c4)
	}
}
