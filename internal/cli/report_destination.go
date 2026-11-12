package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func selectReportDestination(ctx context.Context, streams Command, path string, allowOverwrite, readsStdin bool) (bool, error) {
	overwrite := allowOverwrite
	if path != "" && path != "-" {
		info, e := os.Lstat(path)
		if e == nil {
			if info.IsDir() {
				return false, fmt.Errorf("output file %q is a directory", path)
			}
			if !overwrite {
				// stdin may be a pipe from yes, but never consume workflow input.
				if _, terminal := terminalFile(streams.Stderr); !terminal || readsStdin {
					return false, outputExistsError(path)
				}
				var err error
				overwrite, err = confirmOverwrite(streams, path)
				if err != nil {
					return false, err
				}
				if !overwrite {
					return false, fmt.Errorf("output file %q was not replaced", path)
				}
			}
			if err := ctx.Err(); err != nil {
				return false, err
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return false, e
		}
	}
	return overwrite, nil
}

type reportDestination struct {
	writer    io.Writer
	file      *os.File
	path      string
	overwrite bool
}

func openReportDestination(out io.Writer, path string, overwrite bool) (*reportDestination, error) {
	destination := &reportDestination{writer: out, path: path, overwrite: overwrite}
	if path == "" || path == "-" {
		return destination, nil
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".actionlint-report-*")
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(path); statErr == nil {
		if err := file.Chmod(info.Mode().Perm()); err != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
			return nil, err
		}
	}
	destination.writer, destination.file = file, file
	return destination, nil
}

// finish publishes only a complete report, and always removes its temporary file.
func (d *reportDestination) finish(err error) error {
	if d.file == nil {
		return err
	}
	defer os.Remove(d.file.Name())
	err = errors.Join(err, d.file.Close())
	if err != nil {
		return err
	}
	if d.overwrite {
		return os.Rename(d.file.Name(), d.path)
	}
	if err := publishNewReport(d.file.Name(), d.path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return outputExistsError(d.path)
		}
		return err
	}
	return nil
}

func outputExistsError(path string) error {
	return fmt.Errorf("output file %q already exists; use --yes to replace it", path)
}

func confirmOverwrite(streams Command, path string) (bool, error) {
	if _, err := fmt.Fprintf(streams.Stderr, "%q already exists. Overwrite? [y/N] ", path); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(streams.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.TrimSpace(answer)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

func sameCommandFile(a, b string) bool {
	if absPath(a) == absPath(b) {
		return true
	}
	aInfo, aErr := os.Stat(a)
	bInfo, bErr := os.Stat(b)
	return aErr == nil && bErr == nil && os.SameFile(aInfo, bInfo)
}

func absPath(path string) string {
	if p, err := filepath.Abs(path); err == nil {
		return p
	}
	return path
}
