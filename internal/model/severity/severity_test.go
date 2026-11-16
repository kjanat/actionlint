package severity

import "testing"

func TestParse(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]Severity{
		"info": Info, "note": Info, "hint": Info,
		"warning": Warning, "warn": Warning,
		"error": Error,
	} {
		if got, ok := Parse(text); !ok || got != want {
			t.Errorf("Parse(%q) = %q, %t, want %q, true", text, got, ok, want)
		}
	}
	for _, text := range []string{"", "Error", "fatal", "style"} {
		if got, ok := Parse(text); ok {
			t.Errorf("Parse(%q) = %q, true, want false", text, got)
		}
	}
}

func TestEffectiveAndRank(t *testing.T) {
	t.Parallel()
	if got := Severity("").Effective(); got != Error {
		t.Fatalf("zero Effective() = %q, want error", got)
	}
	if Info.Rank() >= Warning.Rank() || Warning.Rank() >= Error.Rank() || Severity("").Rank() != 0 {
		t.Fatal("ranks are not ordered info < warning < error")
	}
}
