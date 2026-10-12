package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"time"

	"actionlint.kjanat.dev"
	"golang.org/x/sys/execabs"
)

type terminalJSONOutput struct {
	io.Writer
	ctx   context.Context
	color actionlint.ColorOptionKind
}

func (out *terminalJSONOutput) writeJSON(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeTerminalJSON(out.ctx, out.Writer, out.color, append(data, '\n'))
}

func writeTerminalJSON(ctx context.Context, out io.Writer, mode actionlint.ColorOptionKind, data []byte) error {
	file, terminal := terminalFile(out)
	if terminal {
		if jq, err := execabs.LookPath("jq"); err == nil {
			colored := mode == actionlint.ColorOptionKindAlways || (mode == actionlint.ColorOptionKindAuto && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb")
			if formatted, err := formatJSONWithJQ(ctx, jq, data, colored); err == nil {
				data = formatted
				if colored {
					restore := enableTerminalVT(file)
					defer restore()
				}
			}
		}
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
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
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	color := "--monochrome-output"
	if colored {
		color = "--color-output"
	}
	cmd := exec.CommandContext(ctx, jq, color, ".")
	cmd.Stdin = bytes.NewReader(data)
	cmd.WaitDelay = 100 * time.Millisecond
	output, err := cmd.Output()
	if err == nil && len(bytes.TrimSpace(output)) == 0 {
		err = io.ErrUnexpectedEOF
	}
	return output, err
}
