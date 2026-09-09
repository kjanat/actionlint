package main

import (
	"context"
	"encoding/binary"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

func utf16LE(s string) []byte {
	var b []byte
	for _, unit := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, unit)
	}
	return b
}

func TestWSLDistributions(t *testing.T) {
	want := []string{"Ubuntu", "Custom Nix 🐧"}
	for _, data := range [][]byte{
		[]byte("Ubuntu\r\nCustom Nix 🐧\r\n"),
		[]byte("\ufeffUbuntu\n\nCustom Nix 🐧\n"),
		utf16LE("Ubuntu\r\nCustom Nix 🐧\r\n"),
		utf16LE("\ufeffUbuntu\r\nCustom Nix 🐧\r\n"),
	} {
		got, err := wslDistributions(data)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("decoded %q as %q, %v; want %q", data, got, err, want)
		}
	}
	if _, err := wslDistributions([]byte{'N', 0, 'i'}); err == nil {
		t.Fatal("accepted truncated UTF-16 output")
	}
}

func TestFindNix(t *testing.T) {
	wslNix := []string{"wsl.exe", "--distribution", "Custom Nix", "--exec", "sh", "-lc", `exec nix "$@"`, "nix"}
	tests := []struct {
		name, goos, command string
		available           []string
		list                []byte
		listErr, probeErr   error
		want                []string
		wantProbes          []string
		wantErr             string
	}{
		{name: "native Linux", goos: "linux", available: []string{"nix", "wsl.exe"}, want: []string{"nix"}},
		{name: "native Windows", goos: "windows", available: []string{"nix", "wsl.exe"}, want: []string{"nix"}},
		{
			name: "override", goos: "windows", command: `'custom launcher' --nix`,
			available: []string{"nix", "custom launcher"}, want: []string{"custom launcher", "--nix"},
		},
		{name: "invalid override", command: "'", wantErr: "invalid command line string"},
		{name: "empty override", command: "   ", wantErr: "expected an executable and optional arguments"},
		{name: "missing override", command: "missing", wantErr: "requested Nix launcher"},
		{name: "Linux does not use WSL", goos: "linux", available: []string{"wsl.exe"}, wantErr: "requires Nix"},
		{name: "WSL absent", goos: "windows", wantErr: "neither Nix nor WSL"},
		{
			name: "enumeration failure", goos: "windows", available: []string{"wsl.exe"},
			listErr: errors.New("WSL unavailable"), wantErr: "could not list WSL",
		},
		{name: "no distributions", goos: "windows", available: []string{"wsl.exe"}, wantErr: "no installed WSL distribution"},
		{
			name: "second distribution has Nix", goos: "windows", available: []string{"wsl.exe"},
			list: utf16LE("Ubuntu\r\nCustom Nix\r\nUnused\r\n"),
			want: wslNix, wantProbes: []string{"Ubuntu", "Custom Nix"},
		},
		{
			name: "skip timed out distribution", goos: "windows", available: []string{"wsl.exe"},
			list: []byte("Ubuntu\nCustom Nix\n"), probeErr: context.DeadlineExceeded,
			want: wslNix, wantProbes: []string{"Ubuntu", "Custom Nix"},
		},
		{
			name: "cancellation stops detection", goos: "windows", available: []string{"wsl.exe"},
			list: []byte("Ubuntu\nCustom Nix\n"), probeErr: context.Canceled,
			wantErr: "context canceled", wantProbes: []string{"Ubuntu"},
		},
		{
			name: "Nix absent in WSL", goos: "windows", available: []string{"wsl.exe"}, list: []byte("Ubuntu\n"),
			wantErr: "no installed WSL distribution", wantProbes: []string{"Ubuntu"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var probes []string
			lookPath := func(name string) (string, error) {
				if slices.Contains(tt.available, name) {
					return name, nil
				}
				return "", exec.ErrNotFound
			}
			output := func(args ...string) ([]byte, error) {
				if slices.Equal(args, []string{"wsl.exe", "--list", "--quiet"}) {
					return tt.list, tt.listErr
				}
				if len(args) != 9 || args[1] != "--distribution" || !slices.Equal(args[3:], append(slices.Clone(wslNix[3:]), "--version")) {
					t.Fatalf("unexpected WSL probe: %q", args)
				}
				probes = append(probes, args[2])
				if args[2] == "Custom Nix" {
					return []byte("nix (Nix) 2.31.2\n"), nil
				}
				if tt.probeErr != nil {
					return nil, tt.probeErr
				}
				return nil, errors.New("nix: command not found")
			}
			got, err := findNix(tt.command, tt.goos, lookPath, output)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got %v, want error containing %q", err, tt.wantErr)
				}
			} else if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
			if !slices.Equal(probes, tt.wantProbes) {
				t.Errorf("probed %q, want %q", probes, tt.wantProbes)
			}
		})
	}
}
