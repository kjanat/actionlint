//go:build !unix

package pluginregistry

import "os"

func openCandidateFile(root *os.Root, name string) (*os.File, error) {
	return root.Open(name)
}
