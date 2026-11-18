//go:build !unix

package hostprocess

func executableBusy(error) bool { return false }
