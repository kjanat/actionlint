package builtin

import (
	"bytes"
	"testing"
)

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
