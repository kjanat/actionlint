package python

import (
	"testing"
)

func TestQuotedPythonShells(t *testing.T) {
	for _, tc := range []struct{ shell, target string }{
		{`"python" {0}`, ""},
		{`'python' {0}`, ""},
		{`"C:\Program Files\Python\python.exe" {0}`, ""},
		{`"/opt/Python Tools/python3.9" -u {0}`, "py39"},
		{`"C:\Program Files\Python\py.exe" -V:3.10 {0}`, "py310"},
		{`"actions-shells" py -3.9 {0}`, "py39"},
	} {
		if !IsShell(tc.shell) || ShellTarget(tc.shell) != tc.target {
			t.Errorf("quoted shell %q skipped or target changed: %q", tc.shell, ShellTarget(tc.shell))
		}
	}
	for _, shell := range []string{`"python {0}`, `"python"suffix {0}`, `"/opt/python folder/bash" {0}`, `"" {0}`, `"python.exe.other" {0}`} {
		if IsShell(shell) {
			t.Errorf("invalid shell accepted: %q", shell)
		}
	}
}

func TestAdapterTargetRequiresSupportedShell(t *testing.T) {
	for _, shell := range []string{
		"", "actions-shells py -3.9", "actions-shells py -3.9 {0} later.py",
		"actions-shells python3 -3.9 {0}", "actions-shells PY -3.9 {0}",
		"actions-shells -- py -3.9 {0}", "wrapper py -3.9 {0}",
		"actions-shells py -3.9 {0}; echo other", "actions-shells py '-3.9 {0}",
		"actions-shells $RUNTIME -3.9 {0}",
	} {
		t.Run(shell, func(t *testing.T) {
			if IsShell(shell) || ShellTarget(shell) != "" {
				t.Fatalf("unsupported adapter inferred a Python target: %q", shell)
			}
		})
	}
}
