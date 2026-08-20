//go:build !unix

package main

import "os"

func inheritOwner(root *os.Root, paths []string) error {
	return nil
}
