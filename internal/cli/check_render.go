package cli

import (
	"bytes"
	"context"
	"io"
	"os"

	"actionlint.kjanat.dev/v2/internal/buildinfo"
	"actionlint.kjanat.dev/v2/internal/render"
	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
)

func renderCheckResult(ctx context.Context, out io.Writer, r renderOptions, legacy bool, renderer *render.Renderer, result render.Result) error {
	var err error
	jsonColor := r.Color
	r.resolveGitHubActionsColor(out)
	jsonDestination := out
	_, terminal := terminalFile(out)
	prettyJSON := (terminal || r.PrettyJSONExplicit) && r.PrettyJSON && r.Template == "" && (r.Format == render.FormatJSON || r.Format == render.FormatSARIF)
	if legacy {
		out = legacyColorOutput(out, r.Color)
	}
	if !legacy {
		renderer, err = newResultRenderer(r)
		if err != nil {
			return err
		}
		previous := color.NoColor
		defer func() { color.NoColor = previous }()
		switch r.Color {
		case render.ColorNever:
			color.NoColor = true
		case render.ColorAlways:
			color.NoColor = false
		}
		if file, ok := out.(*os.File); ok && !color.NoColor {
			out = colorable.NewColorable(file)
		}
	}
	var writes *commandResultWriter
	if !legacy || r.OutputFile != "" {
		writes = &commandResultWriter{Writer: out}
		out = writes
	}
	if prettyJSON {
		var data bytes.Buffer
		if err := renderer.Render(&data, result); err != nil {
			return err
		}
		if err := writePrettyJSON(ctx, jsonDestination, jsonColor, data.Bytes(), r.PrettyJSONExplicit); err != nil {
			return err
		}
	} else if err := renderer.Render(out, result); err != nil {
		return err
	}
	if writes != nil && writes.err != nil {
		return writes.err
	}
	return nil
}

func newResultRenderer(options renderOptions) (*render.Renderer, error) {
	renderer, err := render.New(options.Format, options.Template, options.Oneline, buildinfo.Version, nil)
	if err != nil {
		return nil, err
	}
	if options.Width != 0 {
		if err := renderer.SetWidth(options.Width); err != nil {
			return nil, err
		}
	}
	return renderer, nil
}

func legacyColorOutput(out io.Writer, mode render.Color) io.Writer {
	if mode == render.ColorNever {
		color.NoColor = true
	} else {
		if mode == render.ColorAlways {
			color.NoColor = false
		}
		if file, ok := out.(*os.File); ok {
			out = colorable.NewColorable(file)
		}
	}
	return out
}
