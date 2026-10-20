package cli

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestSharedPluginAndBuiltinLogWriter(t *testing.T) {
	var buffer bytes.Buffer
	writer := &synchronizedLogWriter{out: &buffer}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				if _, err := writer.Write([]byte("plugin or builtin\n")); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	if count := strings.Count(buffer.String(), "plugin or builtin\n"); count != 800 {
		t.Fatalf("lines=%d", count)
	}
}
