package cli

import "os"

func publishNewReport(source, destination string) error {
	if err := renameNewReport(source, destination); err != nil {
		return &os.LinkError{Op: "publish", Old: source, New: destination, Err: err}
	}
	return nil
}
