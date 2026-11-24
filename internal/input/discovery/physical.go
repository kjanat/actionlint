package discovery

import "path/filepath"

func PhysicalRepositoryRoot(path string) string {
	root := RepositoryRoot(path)
	if root == "" {
		return ""
	}
	if physical, err := filepath.EvalSymlinks(root); err == nil {
		return physical
	}
	return root
}
