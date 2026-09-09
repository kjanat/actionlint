package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/mattn/go-shellwords"
)

func (r *repo) nixCommand(command string) ([]string, error) {
	return findNix(command, runtime.GOOS, exec.LookPath, func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(r.ctx, 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = r.root
		out, err := cmd.Output()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return out, err
	})
}

func findNix(command, goos string, lookPath func(string) (string, error), output func(...string) ([]byte, error)) ([]string, error) {
	if command != "" {
		args, err := shellwords.Parse(command)
		if err != nil || len(args) == 0 {
			return nil, fmt.Errorf("invalid Nix command %q: expected an executable and optional arguments", command)
		}
		if _, err := lookPath(args[0]); err != nil {
			return nil, fmt.Errorf("could not find the requested Nix launcher: %w", err)
		}
		return args, nil
	}
	if nix, err := lookPath("nix"); err == nil {
		return []string{nix}, nil
	}
	if goos != "windows" {
		return nil, errors.New("release verification requires Nix; install it or set -nix-command to a Nix launcher")
	}
	wsl, err := lookPath("wsl.exe")
	if err != nil {
		return nil, errors.New("release verification requires Nix, but neither Nix nor WSL was found")
	}
	listed, err := output(wsl, "--list", "--quiet")
	if err != nil {
		return nil, fmt.Errorf("could not list WSL distributions to find Nix: %w", err)
	}
	distros, err := wslDistributions(listed)
	if err != nil {
		return nil, err
	}
	for _, distro := range distros {
		// Load the login profile so single-user Nix installations are on PATH too.
		args := []string{wsl, "--distribution", distro, "--exec", "sh", "-lc", `exec nix "$@"`, "nix"}
		if _, err := output(append(args, "--version")...); err == nil {
			return args, nil
		} else if errors.Is(err, context.Canceled) {
			return nil, err
		}
	}
	return nil, errors.New("no installed WSL distribution could run Nix; install Nix in WSL or select a launcher with -nix-command")
}

func wslDistributions(b []byte) ([]string, error) {
	// WSL writes UTF-16LE to redirected output; newer versions can also emit UTF-8.
	if bytes.Contains(b, []byte{0}) || bytes.HasPrefix(b, []byte{0xff, 0xfe}) {
		if len(b)%2 != 0 {
			return nil, errors.New("incomplete UTF-16 distribution names from WSL")
		}
		units := make([]uint16, len(b)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(b[i*2:])
		}
		b = []byte(string(utf16.Decode(units)))
	}
	var names []string
	for line := range strings.SplitSeq(strings.TrimPrefix(string(b), "\ufeff"), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}
