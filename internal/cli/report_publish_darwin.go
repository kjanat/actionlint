package cli

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func renameNewReport(source, destination string) error {
	err := unix.RenamexNp(source, destination, unix.RENAME_EXCL)
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) {
		return os.Link(source, destination)
	}
	return err
}
