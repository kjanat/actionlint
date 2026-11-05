package genfile

import "os"

// writeNewFile creates a snapshot without allowing callers to change access flags.
func writeNewFile(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, err = f.Write(content)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
