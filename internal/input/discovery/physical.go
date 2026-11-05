package discovery

import "path/filepath"

// PhysicalRepositoryRoot resolves a discovered repository root through filesystem links.
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
