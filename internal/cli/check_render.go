package cli

import (
	"bytes"
	"context"
	"io"
	"os"

	"actionlint.kjanat.dev/v2/internal/integrations/buildinfo"
	"actionlint.kjanat.dev/v2/internal/model/outputformat"
	"actionlint.kjanat.dev/v2/internal/output"
	"github.com/mattn/go-colorable"
)

type resultRenderer struct {
	format   outputformat.Format
	renderer *output.Renderer
}

func renderCheckResult(ctx context.Context, out io.Writer, r renderOptions, legacy bool, renderers []resultRenderer, result output.RenderedReport) error {
	for _, output := range renderers {
		if err := renderCheckFormat(ctx, out, r, output.format, legacy, output.renderer, result); err != nil {
			return err
		}
	}
	return nil
}

func renderCheckFormat(ctx context.Context, out io.Writer, r renderOptions, format outputformat.Format, legacy bool, renderer *output.Renderer, result output.RenderedReport) error {
	jsonColor := r.Color
	r.resolveGitHubActionsColor(out, format)
	jsonDestination := out
	file, terminal := terminalFile(out)
	links := format == outputformat.Pretty && r.Hyperlinks.enabled(terminal, os.Getenv)
	renderer.SetHyperlinks(links)
	if terminal && links {
		restore := enableTerminalVT(file)
		defer restore()
	}
	prettyJSON := (terminal || r.PrettyJSONExplicit) && r.PrettyJSON && r.Template == "" && (format == outputformat.JSON || format == outputformat.SARIF)
	colored := renderColor(r.Color, terminal, os.Getenv)
	renderer.SetColor(colored)
	translate := colored
	if legacy {
		translate = r.Color != output.ColorNever
	}
	if file, ok := out.(*os.File); ok && translate && !links {
		out = colorable.NewColorable(file)
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
		formats = []outputformat.Format{""}
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

func newResultRenderer(options renderOptions, format outputformat.Format) (*output.Renderer, error) {
	renderer, err := output.New(format, options.Template, options.Oneline, buildinfo.Version, nil)
	if err != nil {
		return nil, err
	}
	renderer.SetShowDependencies(options.DependencyFindings == "full")
	if options.Width != 0 {
		if err := renderer.SetWidth(options.Width); err != nil {
			return nil, err
		}
	}
	return renderer, nil
}

func renderColor(mode output.Color, terminal bool, getenv func(string) string) bool {
	switch mode {
	case output.ColorAlways:
		return true
	case output.ColorNever:
		return false
	case output.ColorAuto:
	}
	return terminal && getenv("NO_COLOR") == "" && getenv("TERM") != "dumb"
}
