package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"actionlint.kjanat.dev/v2/internal/render"

	"golang.org/x/sys/execabs"
)

type prettyJSONOutput struct {
	io.Writer
	ctx      context.Context
	color    render.Color
	explicit bool
}

func (out *prettyJSONOutput) writeJSON(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writePrettyJSON(out.ctx, out.Writer, out.color, append(data, '\n'), out.explicit)
}

func writePrettyJSON(ctx context.Context, out io.Writer, mode render.Color, data []byte, explicit bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if explicit {
		var formatted bytes.Buffer
		if err := json.Indent(&formatted, bytes.TrimSpace(data), "", "  "); err != nil {
			return err
		}
		data = append(formatted.Bytes(), '\n')
	}
	file, terminal := terminalFile(out)
	if terminal {
		if jq, err := execabs.LookPath("jq"); err == nil {
			colored := mode == render.ColorAlways || (mode == render.ColorAuto && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb")
			// Formatting is optional; external startup must not block output.
			formatterCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			formatted, err := formatJSONWithJQ(formatterCtx, jq, data, colored)
			cancel()
			if err == nil {
				data = formatted
				if colored {
					restore := enableTerminalVT(file)
					defer restore()
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := out.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func formatJSONWithJQ(ctx context.Context, jq string, data []byte, colored bool) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	color := "--monochrome-output"
	if colored {
		color = "--color-output"
	}
	cmd := exec.CommandContext(ctx, jq, color, ".")
	cmd.Stdin = bytes.NewReader(data)
	cmd.WaitDelay = 100 * time.Millisecond
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) != 0 {
			return nil, fmt.Errorf("jq: %w: %s", err, bytes.TrimSpace(exitErr.Stderr))
		}
		return nil, err
	}
	if len(bytes.TrimSpace(output)) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	return output, nil
}
