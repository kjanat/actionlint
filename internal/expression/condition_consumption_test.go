package expression

import (
	"strings"
	"testing"
)

func TestConditionSourceFullConsumption(t *testing.T) {
	for _, source := range []string{
		"${{ 'github.ref }} ignored' }}",
		"${{ 'false }}' }}",
		"github.ref }} ignored",
	} {
		t.Run(source, func(t *testing.T) {
			expr, err, _ := ParseConditionExpression(source)
			if err == nil || expr != nil {
				t.Fatalf("accepted incomplete condition: expr=%v err=%v", expr, err)
			}
			if !strings.Contains(err.Message, "unexpected") {
				t.Fatal(err)
			}
		})
	}
}
