package schema

import "testing"

func TestDependencySettingsSchema(t *testing.T) {
	t.Parallel()
	schema := compile(t)
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"lint":{"dependencies":{"fail-on-all":true,"fail-on-categories":["security"],"fail-on-rules":["shellcheck/SC2086"]}}}`, true},
		{`{"overrides":[{"includes":["release.yml"],"lint":{"dependencies":{"fail-on-all":false,"fail-on-categories":[],"fail-on-rules":[]}}}]}`, true},
		{`{"lint":{"dependencies":{"fail-on-all":"yes"}}}`, false},
		{`{"lint":{"dependencies":{"fail-on-categories":["typo"]}}}`, false},
		{`{"lint":{"dependencies":{"unknown":true}}}`, false},
	} {
		if err := schema.Validate([]byte(tc.body)); (err == nil) != tc.valid {
			t.Errorf("%s: valid=%v, error=%v", tc.body, tc.valid, err)
		}
	}
}
