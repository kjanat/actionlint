package core

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTableBudgetRejectsUnboundedOrOversized(t *testing.T) {
	for _, test := range []struct {
		name   string
		limits []byte
	}{
		{"unbounded", []byte{0, 1}},
		{"oversized maximum", []byte{1, 1, 0x80, 0x20}},
		{"oversized minimum", []byte{1, 0x80, 0x20, 0x80, 0x20}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/wat/table-budget.wasm")
			if err != nil {
				t.Fatal(err)
			}
			modified := append([]byte(nil), data[:8]...)
			for offset := 8; offset < len(data); {
				id := data[offset]
				size, n := binary.Uvarint(data[offset+1:])
				if n <= 0 || size > uint64(len(data)-offset-1-n) {
					t.Fatal("invalid fixture section")
				}
				start := offset + 1 + n
				end := start + int(size)
				payload := data[start:end]
				if id == 4 {
					payload = append([]byte{1, 0x70}, test.limits...)
				}
				modified = append(modified, id)
				modified = binary.AppendUvarint(modified, uint64(len(payload)))
				modified = append(modified, payload...)
				offset = end
			}
			path := filepath.Join(t.TempDir(), "table.wasm")
			if err := os.WriteFile(path, modified, 0o600); err != nil {
				t.Fatal(err)
			}
			engine, err := Load(t.Context(), path, Options{MemoryPages: 2})
			if engine != nil {
				if closeErr := engine.Close(context.WithoutCancel(t.Context())); closeErr != nil {
					t.Error(closeErr)
				}
			}
			if err == nil || !strings.Contains(err.Error(), "table") {
				t.Fatalf("unsafe table admitted: %v", err)
			}
		})
	}
}

func TestTablesAndLinearMemoryShareInstanceBudget(t *testing.T) {
	engine, err := Load(t.Context(), "testdata/wat/table-budget.wasm", Options{MemoryPages: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := engine.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	instance := engine.idle[0]
	for _, test := range []struct {
		name        string
		delta, want uint64
	}{
		{"table_grow", 1, 1},
		{"table_grow", 2047, uint64(^uint32(0))},
		{"table_grow", 2046, 2},
		{"memory_grow", 1, uint64(^uint32(0))},
	} {
		result, err := instance.ExportedFunction(test.name).Call(t.Context(), test.delta)
		if err != nil || len(result) != 1 || result[0] != test.want {
			t.Fatalf("%s(%d) = %v, %v; want %d", test.name, test.delta, result, err, test.want)
		}
	}
	if size := instance.Memory().Size(); size != 1<<16 {
		t.Fatalf("linear memory grew past reserved table budget: %d", size)
	}
}
