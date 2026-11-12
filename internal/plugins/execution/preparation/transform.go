package preparation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	integrationconfig "actionlint.kjanat.dev/v2/internal/plugins/execution/configuration"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/jq"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

// ScriptContext supplies source, interpreter facts and integration settings to
// a preparation transform. EnvironmentStartup declares names without values.
type ScriptContext struct {
	Source             string                     `json:"source"`
	Language           string                     `json:"language"`
	Startup            string                     `json:"startup"`
	EnvironmentStartup string                     `json:"environment_startup"`
	EnvironmentKnown   bool                       `json:"environment_known"`
	Settings           integrationconfig.Settings `json:"settings"`
	ConfigFile         string                     `json:"config_file"`
	Filesystem         bool                       `json:"filesystem"`
	Engine             bool                       `json:"engine"`
	Input              bool                       `json:"input"`
}

// SourcePlan adds generated text to a script and supplies the tool's options.
// Source is never rewritten by a transform, so the host can map every row.
type SourcePlan struct {
	Prefix    string            `json:"source_prefix,omitempty"`
	Startup   string            `json:"startup,omitempty"`
	Config    json.RawMessage   `json:"config,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
	Excluded  []string          `json:"excluded,omitempty"`
	Skip      string            `json:"skip,omitempty"`
}

// SourceTransform is a compiled integration-provided preparation program.
type SourceTransform struct{ program *jq.Program }

func NewSourceTransform(spec *manifests.Transform, programs manifests.Programs) (*SourceTransform, error) {
	if spec == nil {
		return nil, nil
	}
	source, err := manifests.ResolveTransform(spec, programs)
	if err != nil {
		return nil, err
	}
	program, err := jq.New(source, 1<<20)
	if err != nil {
		return nil, err
	}
	return &SourceTransform{program: program}, nil
}

// Apply returns an integration's preparation plan. Without a program it inserts
// the supplied comments and startup text, and passes the supplied options on.
func (t *SourceTransform) Apply(ctx context.Context, input ScriptContext) (SourcePlan, error) {
	if err := ctx.Err(); err != nil {
		return SourcePlan{}, err
	}
	if t == nil {
		startup := input.Startup
		if input.Settings.Language != nil && *input.Settings.Language != input.Language {
			startup = ""
		}
		if input.EnvironmentStartup != "" {
			if startup != "" {
				startup += "; "
			}
			startup += input.EnvironmentStartup
		}
		return SourcePlan{Prefix: input.Settings.Prefix, Startup: startup, Config: input.Settings.Options}, nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return SourcePlan{}, err
	}
	output, err := t.program.ApplyJSON(ctx, data)
	if err != nil {
		return SourcePlan{}, err
	}
	if len(output) == 0 || bytes.TrimSpace(output)[0] != '{' {
		return SourcePlan{}, errors.New("preparation transform must return an object")
	}
	var plan SourcePlan
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return SourcePlan{}, err
	}
	if bytes.Equal(bytes.TrimSpace(plan.Config), []byte("null")) {
		plan.Config = nil
	}
	if plan.Prefix != "" && !strings.HasSuffix(plan.Prefix, "\n") {
		return SourcePlan{}, errors.New("preparation transform source_prefix must end with a newline")
	}
	return plan, nil
}
