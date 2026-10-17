package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestCommandLogRecords(t *testing.T) {
	var out bytes.Buffer
	w := commandJSONLogWriter{out: &out}
	_, _ = io.WriteString(&w, "first ")
	_, _ = io.WriteString(&w, "line\nsecond line\n")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() { _, _ = fmt.Fprintf(&w, "worker %d\n", i) })
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 22 {
		t.Fatal(len(lines))
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatal(line)
		}
	}
}
