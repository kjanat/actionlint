package shellcheck

import (
	"encoding/json"
	"errors"

	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/profile"
	shellprofile "actionlint.kjanat.dev/v2/internal/tools/profile/shell"
)

func excludedChecks(settings *shellprofile.Settings) []string {
	if settings == nil {
		return shellprofile.Excluded(nil)
	}
	return shellprofile.Excluded(settings.Exclude)
}

func backendOptions(dialect string, settings *shellprofile.Settings) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"shell": dialect, "exclude": excludedChecks(settings), "external-sources": false})
}

func prepareInput(script tools.Script) (profile.Prepared, error) {
	dialect := ""
	if script.Input != nil {
		dialect = shellprofile.LanguageDialect(script.Input.Language)
	}
	if script.Config != nil && script.Config.Tools["shellcheck"].Config.Path() != "" {
		return profile.Prepared{}, errors.New("ShellCheck backend needs inline configuration; rc-file access requires a process backend")
	}
	inline, err := inlineConfig(script.Config)
	if err != nil {
		return profile.Prepared{}, err
	}
	if inline != nil {
		if len(inline.SourcePath) != 0 || inline.ExternalSources != nil && *inline.ExternalSources {
			return profile.Prepared{}, errors.New("ShellCheck backend does not provide external source access")
		}
		if inline.Shell != nil {
			dialect = *inline.Shell
		}
	}
	directives, err := inline.Directives()
	if err != nil {
		return profile.Prepared{}, err
	}
	prepared, dialect := shellprofile.Input(script.Text, dialect, directives)
	options, err := backendOptions(dialect, nil)
	if err != nil {
		return profile.Prepared{}, err
	}
	prepared.Config = options
	return prepared, nil
}
