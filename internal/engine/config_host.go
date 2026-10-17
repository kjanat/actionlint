package engine

import (
	repoconfig "actionlint.kjanat.dev/internal/config"
)

func diagnosticLevel(cfg *Config, name string) RuleLevel {
	return repoconfig.DiagnosticLevel(cfg, name)
}
func configForDocument(cfg *Config, path, root string) (*Config, error) {
	return repoconfig.ForDocument(cfg, path, root)
}

func writeDefaultConfigFile(path string) error { return repoconfig.WriteDefaultFile(path, Version()) }
