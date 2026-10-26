package architecture_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzers/architecture"
)

const example = `{"module":"example.org/project","layers":[
 {"name":"values","roots":["values"],"uses":[],"portable":true},
 {"name":"domain","roots":["internal/domain"],"uses":["values"],"portable":true},
 {"name":"app","roots":["."],"uses":["domain"]},
 {"name":"tools","roots":["internal/domain/generate"],"uses":["app"],"entrypoint":true}
]}`

func TestDependencyContract(t *testing.T) {
	c, err := architecture.Parse(strings.NewReader(example))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		from, to string
		allowed  bool
	}{
		{"internal/domain/new/subpackage", "values/new", true},
		{"internal/domain/new", "internal/domain/other", true},
		{".", "values/new", true},
		{"values/new", "internal/domain", false},
		{"internal/domain", "internal/domain/generate", false},
		{"internal/domain/generate/new", "internal/domain", true},
		{"internal/domain", "internal/domainish", false},
		{"new", "values", false},
		{"new", "new/child", false},
		{"values", "new", false},
	} {
		t.Run(tt.from+"->"+tt.to, func(t *testing.T) {
			full := func(p string) string {
				if p == "." {
					return c.Module()
				}
				return c.Module() + "/" + p
			}
			if got := c.Allows(full(tt.from), full(tt.to)); got != tt.allowed {
				t.Fatalf("Allows = %t, want %t", got, tt.allowed)
			}
		})
	}
	for _, p := range []string{"example.org/projectile/values", "example.org/project/new"} {
		if _, ok := c.Layer(p); ok {
			t.Fatalf("unexpected classification of %s", p)
		}
	}
	l, ok := c.Layer("example.org/project/internal/domain/new")
	if !ok || !l.Portable || l.EntryPoint {
		t.Fatalf("subtree did not inherit domain properties: %+v", l)
	}
	l, ok = c.Layer("example.org/project/internal/domain/generate/new")
	if !ok || l.Portable || !l.EntryPoint {
		t.Fatalf("nested override not applied: %+v", l)
	}
}

func TestInvalidContracts(t *testing.T) {
	for _, tt := range []struct{ name, document string }{
		{"empty", "{}"},
		{"missing layers", `{"module":"example.org/project","layers":[]}`},
		{"unknown field", strings.Replace(example, `"portable":true`, `"portabel":true`, 1)},
		{"unknown layer", strings.Replace(example, `"uses":["values"]`, `"uses":["typo"]`, 1)},
		{"cycle", strings.Replace(example, `"uses":[]`, `"uses":["domain"]`, 1)},
		{"self cycle", strings.Replace(example, `"uses":[]`, `"uses":["values"]`, 1)},
		{"duplicate root", strings.Replace(example, `"roots":["internal/domain"]`, `"roots":["values"]`, 1)},
		{"duplicate layer", strings.Replace(example, `"name":"domain"`, `"name":"values"`, 1)},
		{"no roots", strings.Replace(example, `"roots":["values"]`, `"roots":[]`, 1)},
		{"glob root", strings.Replace(example, `"roots":["values"]`, `"roots":["*"]`, 1)},
		{"traversal root", strings.Replace(example, `"roots":["values"]`, `"roots":["../other"]`, 1)},
		{"absolute root", strings.Replace(example, `"roots":["values"]`, `"roots":["/values"]`, 1)},
		{"duplicate edge", strings.Replace(example, `"uses":["values"]`, `"uses":["values","values"]`, 1)},
		{"trailing input", example + `{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := architecture.Parse(strings.NewReader(tt.document)); err == nil {
				t.Fatal("invalid contract accepted")
			}
		})
	}
}

func TestFingerprintTracksPolicy(t *testing.T) {
	a, err := architecture.Parse(strings.NewReader(example))
	if err != nil {
		t.Fatal(err)
	}
	b, err := architecture.Parse(strings.NewReader(strings.Replace(example, `"portable":true`, `"portable":false`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("contract changes must invalidate cached analyzer results")
	}
	c, err := architecture.Parse(strings.NewReader("\n" + example + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint() != c.Fingerprint() {
		t.Fatal("whitespace should not invalidate the contract cache")
	}
}

func TestProductionCannotUseTestSupport(t *testing.T) {
	_, err := architecture.Parse(strings.NewReader(`{"module":"example.org/project","layers":[
  {"name":"support","roots":["testutil"],"uses":[],"testonly":true},
  {"name":"production","roots":["app"],"uses":["support"]}
 ]}`))
	if err == nil {
		t.Fatal("production may not depend on a test-only layer")
	}
}
