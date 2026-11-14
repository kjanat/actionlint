package cli

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func TestOnlineOfflineFlags(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"", "check", "fix", "pre-commit"} {
		for _, tc := range []struct {
			flag    string
			offline bool
		}{
			{"", false},
			{"--offline", true},
			{"--online", false},
			{"--offline=false", false},
			{"--online=false", true},
		} {
			t.Run(command+"/"+tc.flag, func(t *testing.T) {
				t.Parallel()
				app := newCommandApp(t.Context(), &Command{})
				args := []string{}
				if command != "" {
					args = append(args, command)
				}
				if tc.flag != "" {
					args = append(args, tc.flag)
				}
				app.root.SetArgs(args)
				if err := app.root.ExecuteContext(t.Context()); err != nil {
					t.Fatal(err)
				}
				if err := app.prepareInvocation(); err != nil {
					t.Fatal(err)
				}
				if app.inv.Check.Offline != tc.offline {
					t.Fatalf("offline = %t, want %t", app.inv.Check.Offline, tc.offline)
				}
			})
		}
	}
}

func TestOnlineOfflineConflictAcrossCommandBoundary(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--offline", "--online"},
		{"check", "--offline", "--online"},
		{"--offline", "check", "--online"},
		{"--online", "fix", "--offline"},
		{"pre-commit", "--online", "--offline"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			var out, log bytes.Buffer
			cmd := Command{Stdout: &out, Stderr: &log}
			argv := append([]string{"actionlint", "--no-config"}, args...)
			status := cmd.MainContext(t.Context(), argv)
			if status != 2 || !strings.Contains(log.String(), "--online and --offline cannot be combined") {
				t.Fatalf("status=%d stderr=%s stdout=%s", status, &log, &out)
			}
		})
	}
}

func TestNetworkFlagsAppearInHelpAndCompletion(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"check", "--help"}, {"__complete", "check", "--o"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			var out, log bytes.Buffer
			cmd := Command{Stdout: &out, Stderr: &log}
			status := cmd.MainContext(t.Context(), append([]string{"actionlint"}, args...))
			text := out.String() + log.String()
			if status != 0 {
				t.Fatalf("status=%d output=%s", status, text)
			}
			for _, flag := range []string{"--offline", "--online"} {
				if !strings.Contains(text, flag) {
					t.Errorf("%s missing from output: %s", flag, text)
				}
			}
			if slices.Contains(args, "--help") && strings.Contains(text, "--action-metadata") {
				t.Fatalf("help still advertises the removed flag: %s", text)
			}
		})
	}
}
