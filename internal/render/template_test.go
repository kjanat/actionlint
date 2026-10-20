package render

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestFormatterPrintJSONEncodeError(t *testing.T) {
	f, err := NewFormatter("{{json .}}", func() string { return "" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	err = f.temp.Execute(&b, math.NaN())
	if err == nil {
		t.Fatal("error did not occur", b.String())
	}
	want := "could not encode template value into JSON"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("%q is not contained in error message %q", want, err.Error())
	}
}

func TestFormatterRegisterRuleInParallel(t *testing.T) {
	f, err := NewFormatter("{{json .}}", func() string { return "" }, []Rule{{Name: "seed", Description: "seeded"}})
	if err != nil {
		t.Fatal(err)
	}
	initialRules := len(f.rules.rules)

	rules := make([]Rule, 0, 100)
	for i := range 100 {
		rules = append(rules, Rule{Name: fmt.Sprintf("rule%d", i), Description: fmt.Sprintf("description for rule%d", i)})
	}

	done := make(chan struct{})
	for range 100 {
		go func() {
			for _, r := range rules {
				f.RegisterRule(r)
			}
			done <- struct{}{}
		}()
	}
	for range 100 {
		<-done
	}

	if len(f.rules.rules) != initialRules+len(rules) {
		t.Fatalf("not all rules were registered. %d rules were registered", len(f.rules.rules))
	}
}
