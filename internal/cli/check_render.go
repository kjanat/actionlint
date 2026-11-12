package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"sync"

	"actionlint.kjanat.dev/v2/internal/integrations/buildinfo"
	render "actionlint.kjanat.dev/v2/internal/output"
	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
)

var globalColorMode sync.Mutex

type resultRenderer struct {
	format   render.Format
	renderer *render.Renderer
}

func renderCheckResult(ctx context.Context, out io.Writer, r renderOptions, legacy bool, renderers []resultRenderer, result render.Result) error {
	for _, output := range renderers {
		if err := renderCheckFormat(ctx, out, r, output.format, legacy, output.renderer, result); err != nil {
			return err
		}
	}
	return nil
}

func renderCheckFormat(ctx context.Context, out io.Writer, r renderOptions, format render.Format, legacy bool, renderer *render.Renderer, result render.Result) error {
	jsonColor := r.Color
	r.resolveGitHubActionsColor(out, format)
	jsonDestination := out
	file, terminal := terminalFile(out)
	links := format == render.FormatPretty && r.Hyperlinks.enabled(terminal, os.Getenv)
	renderer.SetHyperlinks(links)
	if terminal && links {
		restore := enableTerminalVT(file)
		defer restore()
	}
	prettyJSON := (terminal || r.PrettyJSONExplicit) && r.PrettyJSON && r.Template == "" && (format == render.FormatJSON || format == render.FormatSARIF)
	globalColorMode.Lock()
	defer globalColorMode.Unlock()
	previous := color.NoColor
	defer func() { color.NoColor = previous }()
	if legacy {
		out = legacyColorOutput(out, r.Color, links)
	}
	if !legacy {
		switch r.Color {
		case render.ColorNever:
			color.NoColor = true
		case render.ColorAlways:
			color.NoColor = false
		case render.ColorAuto:
			// Keep the terminal detection result.
		}
		if file, ok := out.(*os.File); ok && !color.NoColor && !links {
			out = colorable.NewColorable(file)
		}
	}
	writes := &commandResultWriter{Writer: out}
	out = writes
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
	if writes.err != nil {
		return writes.err
	}
	return nil
}

func newResultRenderers(options renderOptions) ([]resultRenderer, error) {
	formats := options.Formats
	if len(formats) == 0 {
		formats = []render.Format{""}
	}
	renderers := make([]resultRenderer, 0, len(formats))
	for _, format := range formats {
		renderer, err := newResultRenderer(options, format)
		if err != nil {
			return nil, err
		}
		renderers = append(renderers, resultRenderer{format: format, renderer: renderer})
	}
	return renderers, nil
}

func newResultRenderer(options renderOptions, format render.Format) (*render.Renderer, error) {
	renderer, err := render.New(format, options.Template, options.Oneline, buildinfo.Version, nil)
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

func legacyColorOutput(out io.Writer, mode render.Color, hyperlinks bool) io.Writer {
	if mode == render.ColorNever {
		color.NoColor = true
	} else {
		if mode == render.ColorAlways {
			color.NoColor = false
		}
		if file, ok := out.(*os.File); ok && !hyperlinks {
			out = colorable.NewColorable(file)
		}
	}
	return out
}
