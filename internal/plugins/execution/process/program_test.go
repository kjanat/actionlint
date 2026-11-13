package process

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/integrations/process"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func inlineBunManifest(t *testing.T, source string) (manifests.Manifest, *process.Command) {
	t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("Bun not installed")
	}
	output, err := exec.CommandContext(t.Context(), bun, "-p", "process.execPath").Output()
	if err != nil {
		t.Fatal(err)
	}
	bun = strings.TrimSpace(string(output))
	manifest := documentManifest(manifests.ProcessSpec{Entry: "bun", Args: []string{"{program-file}"}, Program: &manifests.InlineProgram{Filename: "actionlint.mjs", Inline: source}, Stdin: manifests.StdinRequest, Config: manifests.ConfigRendering{Render: manifests.RenderNone}, Output: manifests.Output{Format: manifests.FormatEnvelope}})
	manifest.Rule.Category, manifest.Rule.Subcategory, manifest.Rule.Maturity, manifest.Rule.Provider = "correctness", "workflow", "stable", "helper"
	manifest.Rule.Description = "Inline runtime fixture"
	manifest.Rule.DocumentationURL = "https://example.com/helper"
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err = manifests.Parse(data)
	if err != nil {
		t.Fatal("standalone manifest rejected", err)
	}
	pool := process.NewProcessPool(t.Context(), 1)
	command, err := pool.ConfiguredCommandRunner("", &processconfig.Options{Executable: &bun}, false)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, command
}

func pathWithBun(t *testing.T) string {
	t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("Bun not installed")
	}
	output, err := exec.CommandContext(t.Context(), bun, "-p", "process.execPath").Output()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(strings.TrimSpace(string(output))) + string(os.PathListSeparator) + os.Getenv("PATH")
}

func programTempRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "program path with whitespace")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func assertProgramsCleaned(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("materialized program debris", entries, err)
	}
}

func TestInlineProgramBunStandaloneRequestAndCleanup(t *testing.T) {
	t.Setenv("PATH", pathWithBun(t))
	temporary := programTempRoot(t)
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, temporary)
	}
	manifest, command := inlineBunManifest(t, `import {readFileSync} from 'node:fs';
const request=JSON.parse(readFileSync(0,'utf8'));
console.log(JSON.stringify({ok:true,diagnostics:[{filename:request.filename,code:'INLINE',message:JSON.stringify({source:request.source,config:request.config,path:process.argv[1],args:process.argv.slice(2)}),location:{row:1,column:1}}]}));`)
	manifest.Process.Args = append(manifest.Process.Args, "one argument with spaces")
	input := documentInput(t, "name: exact ${{ github.actor }}\n")
	input.Options = json.RawMessage(`{"checked":true}`)
	checker, err := NewDocumentAnalyzer(manifest, command, Options{})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := checker.CheckDocument(t.Context(), input)
	if err != nil || len(findings) != 1 {
		t.Fatal(findings, err)
	}
	var reply struct {
		Source string          `json:"source"`
		Config json.RawMessage `json:"config"`
		Path   string          `json:"path"`
		Args   []string        `json:"args"`
	}
	if err := json.Unmarshal([]byte(findings[0].Message), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Source != input.Content || string(reply.Config) != string(input.Options) || filepath.Ext(reply.Path) != ".mjs" || !strings.Contains(reply.Path, "program path with whitespace") || len(reply.Args) != 1 || reply.Args[0] != "one argument with spaces" {
		t.Fatal("standalone source/argv changed", reply)
	}
	if _, err := os.Stat(reply.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("program file not removed", err)
	}
	assertProgramsCleaned(t, temporary)
}

func TestInlineProgramFailureAndCancellationCleanup(t *testing.T) {
	for _, mode := range []string{"failure", "cancellation"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("PATH", pathWithBun(t))
			temporary := programTempRoot(t)
			for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
				t.Setenv(name, temporary)
			}
			source := "process.exit(9);"
			marker := filepath.Join(t.TempDir(), "started")
			if mode == "cancellation" {
				source = `import {writeFileSync} from 'node:fs';writeFileSync(process.argv[2],process.argv[1]);setInterval(()=>{},1000);`
			}
			manifest, command := inlineBunManifest(t, source)
			manifest.Process.Args = append(manifest.Process.Args, marker)
			checker, err := NewDocumentAnalyzer(manifest, command, Options{})
			if err != nil {
				t.Fatal(err)
			}
			input := documentInput(t, "name: captured\n")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancellation" {
				go func() {
					deadline := time.NewTimer(2 * time.Second)
					defer deadline.Stop()
					tick := time.NewTicker(5 * time.Millisecond)
					defer tick.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-deadline.C:
							cancel()
							return
						case <-tick.C:
							if _, err := os.Stat(marker); err == nil {
								cancel()
								return
							}
						}
					}
				}()
			}
			if _, err := checker.CheckDocument(ctx, input); err == nil {
				t.Fatal("failed program returned success")
			}
			if mode == "cancellation" {
				if _, err := os.Stat(marker); err != nil {
					t.Fatal("program never started", err)
				}
			}
			assertProgramsCleaned(t, temporary)
		})
	}
}

func TestInlineProgramCreationFailureCleanup(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	t.Setenv("TMP", temporary)
	t.Setenv("TEMP", temporary)
	path, cleanup, err := materializeProgram(t.Context(), manifests.InlineProgram{Filename: "../escape.mjs", Inline: "exact source\n"})
	if err == nil || path != "" || cleanup != nil {
		t.Fatalf("invalid filename materialized: %q, %v", path, err)
	}
	assertProgramsCleaned(t, temporary)
}

func TestInlineProgramReadOnlyAndEntryProbe(t *testing.T) {
	t.Setenv("PATH", pathWithBun(t))
	temporary := programTempRoot(t)
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, temporary)
	}
	path, cleanup, err := materializeProgram(t.Context(), manifests.InlineProgram{Filename: "source.mjs", Inline: "exact source\n"})
	if err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil || stat.Mode().Perm()&0o222 != 0 {
		t.Fatal("program is writable", stat, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "exact source\n" {
		t.Fatal("program bytes changed", err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	manifest, command := inlineBunManifest(t, "this source is deliberately not valid JavaScript")
	manifest.Process.Version = &manifests.VersionProbe{Args: []string{"--version"}, Pattern: `([0-9.]+)`}
	backend, err := NewBackend(manifest, command, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if version, err := backend.ProbeVersion(t.Context()); err != nil || version == "" {
		t.Fatal("version probe ran program instead of entry", version, err)
	}
	assertProgramsCleaned(t, temporary)
}
