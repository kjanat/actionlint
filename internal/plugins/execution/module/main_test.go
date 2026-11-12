package module_test

import (
	"context"
	"os"
	"testing"

	"github.com/kjanat/wazero"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	runtime := wazero.NewRuntime(ctx)
	if err := runtime.Close(ctx); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
