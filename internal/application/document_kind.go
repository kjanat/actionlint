package application

import (
	"path/filepath"
)

func isActionInput(path string) bool {
	directory := filepath.Dir(path)
	if filepath.Base(directory) == "workflows" && filepath.Base(filepath.Dir(directory)) == ".github" {
		return false
	}
	name := filepath.Base(path)
	return name == "action.yml" || name == "action.yaml"
}
