package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

const commandHelper = "CHECK_README_COMMAND_HELPER"

func TestGeneratorCommandExitOne(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		allowExitOne bool
		wantOutput   string
		wantError    string
	}{
		{
			name:         "actionlint diagnostics",
			mode:         "diagnostic",
			allowExitOne: true,
			wantOutput:   "diagnostic\n",
		},
		{
			name:      "version probe",
			mode:      "diagnostic",
			wantError: "exit status 1",
		},
		{
			name:         "mise failure",
			mode:         "failure",
			allowExitOne: true,
			wantError:    "could not resolve tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(commandHelper, tt.mode)
			g := generator{ctx: context.Background()}
			out, err := g.command(tt.allowExitOne, os.Args[0], "-test.run=^TestCommandHelper$")
			if string(out) != tt.wantOutput {
				t.Fatalf("output was %q but wanted %q", out, tt.wantOutput)
			}
			if tt.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error was %v but wanted it to contain %q", err, tt.wantError)
			}
		})
	}
}

func TestCommandHelper(t *testing.T) {
	switch os.Getenv(commandHelper) {
	case "":
		return
	case "diagnostic":
		fmt.Fprintln(os.Stdout, "diagnostic")
	case "failure":
		fmt.Fprintln(os.Stderr, "could not resolve tool")
	default:
		t.Fatalf("unexpected helper mode %q", os.Getenv(commandHelper))
	}
	os.Exit(1)
}
