package dependencycache

import (
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp"
)

type Acquisition struct {
	UserCacheDir string
	Connections  acquisitionhttp.Connections
}

func (a Acquisition) Directory() (string, error) {
	base := a.UserCacheDir
	if base == "" {
		var err error
		if base, err = os.UserCacheDir(); err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "actionlint"), nil
}
