package builtin

import (
	"bytes"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"
)

func TestRuleConfiguration(t *testing.T) {
	r := Configuration{}
	if r.Config() != nil {
		t.Error("Config must be nil after creating a rule")
	}
	want := &repoconfig.Config{}
	r.SetConfig(want)
	if have := r.Config(); have != want {
		t.Errorf("Wanted config %v but got %v", want, have)
	}
}

func TestRuleLogger(t *testing.T) {
	r := ruleLogger{tag: "dummy-name"}
	r.Debug("this %s output", "is not")

	b := &bytes.Buffer{}
	r.EnableDebug(b)
	r.Debug("this %s output!", "IS")

	have := b.String()
	want := "[dummy-name] this IS output!\n"
	if want != have {
		t.Errorf("wanted %q as debug output but have %q", want, have)
	}
}
