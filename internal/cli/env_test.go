package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestEnvironmentWordsAndChildEnvironment(t *testing.T) {
	t.Setenv("ACTIONLINT_TEST_FORWARD", "inherited")
	for _, tc := range []struct {
		value string
		want  []string
	}{
		{`-e SC2086 --name 'two words'`, []string{"-e", "SC2086", "--name", "two words"}},
		{`["C:\\Program Files\\tool", "", "$VALUE", "$(command)"]`, []string{`C:\Program Files\tool`, "", "$VALUE", "$(command)"}},
	} {
		got, err := parseEnvironmentWords(tc.value)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("words %q: %q, %v; want %q", tc.value, got, err, tc.want)
		}
	}
	for _, value := range []string{`[1]`, `[null]`, `"unterminated`, `ok; other`, `["\u0000"]`} {
		if _, err := parseEnvironmentWords(value); err == nil {
			t.Fatalf("accepted invalid arguments %q", value)
		}
	}
	for _, value := range []string{
		`ACTIONLINT_TEST_FORWARD EMPTY= NAME='value with spaces=here'`,
		`{"ACTIONLINT_TEST_FORWARD":null,"EMPTY":"","NAME":"value with spaces=here"}`,
	} {
		got, err := parseChildEnvironment(value)
		want := []string{"ACTIONLINT_TEST_FORWARD=inherited", "EMPTY=", "NAME=value with spaces=here"}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("environment %q: %q, %v; want %q", value, got, err, want)
		}
	}
	for _, value := range []string{`{"BAD=NAME":"secret"}`, `{"NAME":12}`, `9NAME=secret`, `{"NAME":"\u0000"}`} {
		if _, err := parseChildEnvironment(value); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid environment accepted or value leaked: %v", err)
		}
	}
}
