package shellcheck

import (
	"encoding/json"
	"errors"
	"slices"

	"actionlint.kjanat.dev/v2/internal/tools"
)

func excludedChecks(settings *ShellcheckSettings) []string {
	excluded := []string{"SC1091", "SC2194", "SC2050", "SC2153", "SC2154", "SC2157", "SC2043"}
	if settings != nil {
		for _, code := range settings.Exclude {
			if !slices.Contains(excluded, code) {
				excluded = append(excluded, code)
			}
		}
	}
	return excluded
}

func backendOptions(dialect string, settings *ShellcheckSettings) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"shell": dialect, "exclude": excludedChecks(settings), "external-sources": false})
}

func prepareInput(script tools.Script) (tools.PreparedInput, error) {
	script.Text = sanitizeExpressionsInScript(script.Text)
	dialect := ""
	if script.Input != nil {
		switch script.Input.Language {
		case "bash", "sh", "dash", "ksh", "busybox":
			dialect = script.Input.Language
		}
	}
	if script.Config != nil && script.Config.Tools["shellcheck"].Config.Path() != "" {
		return tools.PreparedInput{}, errors.New("ShellCheck backend needs inline configuration; rc-file access requires a process backend")
	}
	inline, err := inlineConfig(script.Config)
	if err != nil {
		return tools.PreparedInput{}, err
	}
	if inline != nil {
		if len(inline.SourcePath) != 0 || inline.ExternalSources != nil && *inline.ExternalSources {
			return tools.PreparedInput{}, errors.New("ShellCheck backend does not provide external source access")
		}
		if inline.Shell != nil {
			dialect = *inline.Shell
		}
	}
	if header, _, selected := shellcheckHeader(script.Text); selected {
		dialect = shellcheckHeaderDialect(header)
	}
	options, err := backendOptions(dialect, nil)
	if err != nil {
		return tools.PreparedInput{}, err
	}
	directives, err := inline.Directives()
	if err != nil {
		return tools.PreparedInput{}, err
	}
	if directives == "" {
		return tools.PreparedInput{Source: script.Text, Config: options}, nil
	}
	prepared := prepareShellcheckScript(script.Text, directives, "")
	return tools.PreparedInput{Source: prepared.text, Config: options, LineMap: prepared.preparedLine}, nil
}

// preparedLine maps a row of the prepared script to the run block row it
// holds, or to the generated directive or startup line it is.
func (script shellcheckScript) preparedLine(line int) tools.PreparedLine {
	if original := script.originalLine(line); original != 0 {
		return tools.PreparedLine{Line: original, Origin: tools.LineScript}
	}
	if script.startupLine != 0 && line == script.startupLine {
		return tools.PreparedLine{Origin: tools.LineStartup}
	}
	return tools.PreparedLine{Origin: tools.LineDirective}
}
