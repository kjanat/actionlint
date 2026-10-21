package wasmbudget

import (
	"testing"

	"github.com/partite-ai/wacogo"
	"github.com/partite-ai/wacogo/wasmparser"
)

func TestTablesReserveMaximumWithGrowthHeadroom(t *testing.T) {
	for _, test := range []struct {
		name        string
		tables      []wacogo.CoreTable
		limit, want uint64
		valid       bool
	}{
		{"none", nil, 0, 0, true},
		{"empty", []wacogo.CoreTable{{Max: wasmparser.Some(uint32(0))}}, 0, 0, true},
		{"maximum not initial", []wacogo.CoreTable{{Min: 1, Max: wasmparser.Some(uint32(2))}}, 64, 64, true},
		{"aggregate", []wacogo.CoreTable{{Max: wasmparser.Some(uint32(2))}, {Max: wasmparser.Some(uint32(1))}}, 96, 96, true},
		{"aggregate exceeds", []wacogo.CoreTable{{Max: wasmparser.Some(uint32(2))}, {Max: wasmparser.Some(uint32(1))}}, 95, 0, false},
		{"unbounded", []wacogo.CoreTable{{Min: 0}}, 1024, 0, false},
		{"invalid minimum", []wacogo.CoreTable{{Min: 2, Max: wasmparser.Some(uint32(1))}}, 1024, 0, false},
		{"wide multiplication", []wacogo.CoreTable{{Max: wasmparser.Some(^uint32(0))}}, 1 << 32, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, err := Tables(test.tables, test.limit)
			if (err == nil) != test.valid || actual != test.want {
				t.Fatalf("Tables() = %d, %v; want %d valid=%v", actual, err, test.want, test.valid)
			}
		})
	}
}
