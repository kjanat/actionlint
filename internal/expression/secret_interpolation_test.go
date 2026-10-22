package expression

import (
	"strings"
	"testing"
)

func TestSecretInterpolationValueFlow(t *testing.T) {
	for _, tc := range []struct {
		expression string
		count      int
	}{
		{"secrets.TOKEN", 1},
		{"secrets['MixedCaseToken']", 1},
		{"secrets[env.SECRET_NAME]", 1},
		{"github.token", 1},
		{"github.TOKEN", 1},
		{"github['TOKEN']", 1},
		{"github[format('to{0}', 'ken')]", 1},
		{"github[format('to{0}', 'ol')]", 0},
		{"toJSON(secrets)", 1},
		{"format('--token={0}', secrets.TOKEN)", 1},
		{"fromJSON(secrets.CONFIG).token", 1},
		{"secrets.TOKEN || 'fallback'", 1},
		{"env.ENABLED && secrets.TOKEN || 'fallback'", 1},
		{"true && secrets.TOKEN", 1},
		{"false || secrets.TOKEN", 1},
		{"case(env.ENABLED, secrets.TOKEN, github.token)", 2},
		{"case(false, secrets.FIRST, true, secrets.SECOND, secrets.THIRD)", 1},
		{"secrets.TOKEN != ''", 0},
		{"!secrets.TOKEN", 0},
		{"secrets.TOKEN && 'present' || 'missing'", 0},
		{"contains(secrets.TOKEN, 'prefix')", 0},
		{"startsWith(secrets.TOKEN, 'prefix')", 0},
		{"endsWith(secrets.TOKEN, 'suffix')", 0},
		{"hashFiles(secrets.PATH)", 0},
		{"format('{0}', secrets.TOKEN == 'value')", 0},
		{"toJSON(secrets.TOKEN != '')", 0},
		{"false && secrets.TOKEN", 0},
		{"true || secrets.TOKEN", 0},
		{"case(secrets.TOKEN, 'present', 'missing')", 0},
		{"case(true, 'safe', secrets.TOKEN)", 0},
		{"github.repositoryUrl", 0},
		{"matrix.target", 0},
		{"'literal'", 0},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			node, err := NewParser().Parse(NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			findings := CheckSecretInterpolations(node)
			if len(findings) != tc.count {
				t.Fatalf("got %v, want %d secret interpolations", findings, tc.count)
			}
			for _, finding := range findings {
				if finding.Rule != "script-injection" || finding.Code != "secret-interpolation" || !strings.Contains(finding.Message, "environment variable") {
					t.Errorf("wrong diagnostic contract: %+v", finding)
				}
			}
		})
	}
}

func TestSecretInterpolationPreservesName(t *testing.T) {
	node, err := NewParser().Parse(NewLexer("secrets.MixedCaseToken}}"))
	if err != nil {
		t.Fatal(err)
	}
	findings := CheckSecretInterpolations(node)
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "secrets.MixedCaseToken") {
		t.Fatalf("source spelling lost: %v", findings)
	}
}

func TestSecretInterpolationWithIndependentSemanticError(t *testing.T) {
	for _, tc := range []struct {
		expression              string
		wantSecrets, wantErrors int
	}{
		{"secrets.TOKEN || github.events", 1, 1},
		{"github.events || secrets.TOKEN", 1, 1},
		{"format('{0}{1}', secrets.TOKEN, github.events)", 1, 1},
		{"secrets[true]", 0, 1},
		{"secrets.TOKEN.invalid", 0, 1},
		{"github.token.invalid || secrets.TOKEN", 1, 1},
		{"secrets.TOKEN != '' || github.events", 0, 1},
		{"false && secrets.TOKEN || github.events", 0, 1},
		{"secrets.TOKEN", 1, 0},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			node, err := NewParser().Parse(NewLexer(tc.expression + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			checker := NewChecker(true, nil)
			checker.SetWorkflowKeyAvailability("jobs.<job_id>.steps.run")
			_, findings := checker.Check(node)
			secrets, errors := 0, 0
			for _, finding := range findings {
				if finding.Code == "secret-interpolation" {
					secrets++
				} else {
					errors++
				}
			}
			if secrets != tc.wantSecrets || errors != tc.wantErrors {
				t.Fatalf("got %d secret findings and %d semantic findings, want %d and %d: %+v", secrets, errors, tc.wantSecrets, tc.wantErrors, findings)
			}
		})
	}
}
