//go:build !windows

package cli

import "os"

func enableHelpVT(*os.File) func() { return func() {} }
