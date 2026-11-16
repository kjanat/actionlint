package installation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	configplugins "actionlint.kjanat.dev/v2/internal/input/configuration/plugins"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
)

// InitializeConfiguration verifies default plugin artifacts before creating configuration.
// A failed resolution leaves the project unchanged.
func InitializeConfiguration(ctx context.Context, rules configuration.RuleSet, acquisition pluginacquisition.Settings, root, version string, format configuration.FileFormat) (string, error) {
	path, err := projectConfigPath(root, "")
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("config file already exists at %q", path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	path = filepath.Join(filepath.Dir(path), format.Filename())
	project, err := OpenProject(ctx, rules, acquisition, root, path)
	if err != nil {
		return "", err
	}
	if project.Manifest.Exists {
		return "", fmt.Errorf("config file already exists at %q", project.Manifest.Path)
	}
	content, err := rules.DefaultDocument(version, format)
	if err != nil {
		return "", err
	}
	node, err := document.Parse(path, content)
	if err != nil {
		return "", err
	}
	defaults, _, err := configplugins.References(node.Value())
	if err != nil {
		return "", err
	}
	refs := defaults
	if err := project.Resolve(ctx, refs, nil); err != nil {
		return "", err
	}
	project.Manifest.SetContent(content)
	if err := project.Save(ctx, refs); err != nil {
		return "", err
	}
	return project.Manifest.Path, nil
}
