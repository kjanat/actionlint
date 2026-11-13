package typecheck

import (
	"strings"
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func TestConditionSourceFullConsumption(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"${{ 'github.ref }} ignored' }}",
		"${{ 'false }}' }}",
		"github.ref }} ignored",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			expr, err, _ := exprsyntax.ParseConditionExpression(source)
			if err == nil || expr != nil {
				t.Fatalf("accepted incomplete condition: expr=%v err=%v", expr, err)
			}
			if !strings.Contains(err.Message, "unexpected") {
				t.Fatal(err)
			}
		})
	}
}
