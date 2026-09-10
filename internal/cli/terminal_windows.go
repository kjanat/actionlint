package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

func enableHelpVT(file *os.File) func() {
	handle := windows.Handle(file.Fd())
	var mode uint32
	if windows.GetConsoleMode(handle, &mode) != nil || mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return func() {}
	}
	if windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) != nil {
		return func() {}
	}
	return func() { _ = windows.SetConsoleMode(handle, mode) }
}
