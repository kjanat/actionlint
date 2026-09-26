//go:build !unix

package githubaction

import "os"

func inheritOwner(*os.Root, []string) error {
	return nil
}
