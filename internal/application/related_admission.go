package application

import (
	"path/filepath"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	sourceposition "actionlint.kjanat.dev/v2/internal/source/position"
)

func admitRelated(locations []diagnostic.Related, captures *capture.Store, rows map[string]capture.Source, workingDir string) ([]diagnostic.Related, int) {
	if locations == nil {
		return nil, 0
	}
	kept := make([]diagnostic.Related, 0, len(locations))
	withheld := 0
	address := func(path string) string {
		if path == "<stdin>" {
			return path
		}
		if !filepath.IsAbs(path) {
			return filepath.Join(workingDir, path)
		}
		return filepath.Clean(path)
	}
	for _, location := range locations {
		row, exists := rows[location.Revision.Document()]
		if !exists || !location.Revision.Valid() || !location.Coordinates.Valid() || row.State != capture.Captured || row.Revision != location.Revision || row.Coordinates != location.Coordinates || !row.CoordinatesAvailable || location.Path == "" {
			withheld++
			continue
		}
		alias := false
		for _, path := range row.Aliases {
			if address(path) == address(location.Path) {
				alias = true
				break
			}
		}
		snapshot, ok := captures.Revision(location.Revision)
		if !alias || !ok || !snapshot.CoordinatesAvailable() || snapshot.Coordinates() != location.Coordinates {
			withheld++
			continue
		}
		if _, _, err := sourceposition.Range(snapshot.Bytes(), location.Coordinates, location.Start, location.End); err != nil {
			withheld++
			continue
		}
		kept = append(kept, location)
	}
	return kept, withheld
}
