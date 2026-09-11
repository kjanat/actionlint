//go:build !windows

package cli

import "os"

func enableTerminalVT(*os.File) func() { return func() {} }
