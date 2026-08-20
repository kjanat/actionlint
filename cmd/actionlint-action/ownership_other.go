//go:build !unix

package main

func inheritOwner(workspace string, paths []string) error {
	return nil
}
