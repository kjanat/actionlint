//go:build !linux && !darwin && !windows

package cli

import "os"

func renameNewReport(source, destination string) error {
	return os.Link(source, destination)
}
