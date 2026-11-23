package module

import (
	"fmt"

	"github.com/kjanat/wacogo"
)

// ReservedTableBytes reserves declared maxima, including room for old backing storage,
// append capacity and the temporary slice used by wazero table.grow. Each table
// reference occupies at most eight bytes on supported hosts. Imported tables
// must be excluded by the caller. Imported tables alias their defining instance's charge.
func ReservedTableBytes(tables []wacogo.CoreTable, limit uint64) (uint64, error) {
	const bytesPerMaximumEntry = uint64(4 * 8)
	var total uint64
	for i, table := range tables {
		if !table.Max.Valid {
			return 0, fmt.Errorf("table %d has no finite maximum", i)
		}
		if table.Min > table.Max.Value {
			return 0, fmt.Errorf("table %d minimum exceeds its maximum", i)
		}
		size := uint64(table.Max.Value) * bytesPerMaximumEntry
		if size > limit-total {
			return 0, fmt.Errorf("table %d exceeds the plugin memory budget", i)
		}
		total += size
	}
	return total, nil
}
