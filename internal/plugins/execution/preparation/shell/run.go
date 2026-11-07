package shell

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/commands"

	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"

	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation/mask"
)

// Argument variables the profile sets for process tools.
const (
	// VarDialect is the dialect a tool should assume. It is unset when a
	// leading `# shellcheck shell=` directive selects the dialect itself.
	VarDialect = "dialect"
	// VarExclude lists the excluded check codes, one per line.
	VarExclude = "exclude"
	// VarConfigFile is the native configuration file; unset runs without one.
	VarConfigFile = "config_file"
	// VarExternalSources is "true" when the tool may follow sourced files.
	VarExternalSources = "external_sources"
)

// ErrNoSourceAccess rejects source following for a snippet engine, which
// reads no files.
var ErrNoSourceAccess = errors.New("a snippet engine does not provide external source access")

// Run selects how a run block is prepared for a tool.
type Run struct {
	Shell Shell
	// Directives are the inline settings, their source paths already
	// expanded; nil selects none.
	Directives *Directives
	// ConfigFile is the resolved native configuration file, or "".
	ConfigFile string
	// Sources permits following sourced files: the working directory is
	// known and the transport reads the filesystem. Directives can only turn
	// it off.
	Sources bool
	// Engine prepares for a snippet engine, which receives its dialect and
	// exclusions as configuration and follows no sources.
	Engine      bool
	Environment []scriptmodel.EnvironmentNames
}

// PrepareRun masks templates in a run block and inserts the inline directives
// and the startup command reproducing the runner's shell options. A script
// whose dialect neither the step shell nor a leading directive selects is
// skipped. A dialect other than the inferred one drops the startup command,
// whose options may not exist in it.
func PrepareRun(r Run, text string) (preparation.Prepared, error) {
	invocation := r.Shell.Invocation()
	if invocation.Status == commands.Invalid {
		return preparation.Prepared{Skip: "invalid interpreter invocation: " + invocation.Issues[0].Message}, nil
	}
	dialect, setup := r.Shell.Analysis()
	header, _, directiveShell := Header(text)
	if dialect == "" && !directiveShell {
		return preparation.Prepared{Skip: fmt.Sprintf("cannot infer a supported script dialect from shell %q; a leading # shellcheck shell=bash (or another supported dialect) selects it explicitly", r.Shell.Name)}, nil
	}
	inferred := dialect
	switch {
	case directiveShell:
		dialect = HeaderDialect(header)
	case r.Directives != nil && r.Directives.Shell != nil:
		dialect = *r.Directives.Shell
	}
	if dialect != inferred {
		setup = ""
	}
	sources := r.Sources && !r.Engine
	if r.Directives != nil && r.Directives.ExternalSources != nil {
		sources = sources && *r.Directives.ExternalSources
	}
	directives, err := RenderDirectives(r.Directives)
	if err != nil {
		return preparation.Prepared{}, err
	}
	if !sources {
		directives += "# shellcheck external-sources=false\n"
	}
	setup, environmentExcluded := environmentStartup(setup, r.Environment)
	excluded := Excluded(environmentExcluded)
	script := Prepare(mask.Expressions(text), directives, setup)
	prepared := preparation.Prepared{Source: script.Text(), LineMap: script.Line, Excluded: excluded}
	if r.Engine {
		prepared.Config, err = EngineConfig(dialect, excluded)
		return prepared, err
	}
	prepared.Vars = map[string]string{VarExclude: strings.Join(excluded, "\n")}
	if !directiveShell && dialect != "" {
		prepared.Vars[VarDialect] = dialect
	}
	if r.ConfigFile != "" {
		prepared.Vars[VarConfigFile] = r.ConfigFile
	}
	if sources {
		prepared.Vars[VarExternalSources] = "true"
	}
	return prepared, nil
}

// EngineInput prepares an action input for a snippet engine, which reads no
// files: it masks templates, lets a leading `# shellcheck shell=` directive
// select the dialect over the declared one and inserts the inline
// directives. The engine configuration names the dialect and the excluded
// checks.
func EngineInput(text, dialect string, directives *Directives, environment []scriptmodel.EnvironmentNames) (preparation.Prepared, error) {
	if directives != nil {
		if len(directives.SourcePath) != 0 || directives.ExternalSources != nil && *directives.ExternalSources {
			return preparation.Prepared{}, ErrNoSourceAccess
		}
		if directives.Shell != nil {
			dialect = *directives.Shell
		}
	}
	rendered, err := RenderDirectives(directives)
	if err != nil {
		return preparation.Prepared{}, err
	}
	startup, environmentExcluded := environmentStartup("", environment)
	prepared, dialect := Input(text, dialect, rendered, startup)
	prepared.Excluded = Excluded(environmentExcluded)
	prepared.Config, err = EngineConfig(dialect, prepared.Excluded)
	return prepared, err
}

// EngineConfig is a snippet engine's configuration: the dialect, the
// excluded checks and no source following.
func EngineConfig(dialect string, excluded []string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"shell": dialect, "exclude": excluded, "external-sources": false})
}
