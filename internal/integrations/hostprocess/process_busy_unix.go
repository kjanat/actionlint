//go:build unix

package hostprocess

import (
	"errors"
	"syscall"
)

func executableBusy(err error) bool { return errors.Is(err, syscall.ETXTBSY) }
