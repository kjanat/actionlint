package cli

import (
	"context"
	"testing"
)

func TestUnknownOperation(t *testing.T) {
	t.Parallel()
	_, err := executeInvocation(context.Background(), Command{}, invocation{Operation: "typo"})
	if err == nil {
		t.Fatal("unknown operation executed")
	}
}
