package genfile

import "os"

func writeNewFile(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
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
