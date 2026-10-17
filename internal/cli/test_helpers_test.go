package cli

import (
	"errors"
)

type commandFailingIO struct{}

func (commandFailingIO) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func (commandFailingIO) Write([]byte) (int, error) { return 0, errors.New("write failed") }
