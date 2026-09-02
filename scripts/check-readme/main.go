// check-readme keeps the demo section of README.md in sync with the fixture it describes.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/google/go-cmp/cmp"
)

const (
	beginMarker  = "<!-- BEGIN generated demo -->"
	endMarker    = "<!-- END generated demo -->"
	fixtureDir   = "docs/screenshots"
	workflow     = "demo-workflow.yaml"
	config       = "actionlint.yaml"
	upstreamTool = "actionlint@latest"
)

var (
	diagnostic = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(workflow) + `:\d+:\d+:`)
	ruleName   = regexp.MustCompile(`\[([a-z0-9-]+)\]$`)
)

// summarise counts the diagnostics and names the rules that produced them, in the order they first
// appear, so the two blocks can be told apart without reading either in full.
func summarise(out string) string {
	var order []string
	seen := map[string]int{}
	for line := range strings.SplitSeq(out, "\n") {
		m := ruleName.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		if _, ok := seen[m[1]]; !ok {
			order = append(order, m[1])
		}
		seen[m[1]]++
	}
	parts := make([]string, 0, len(order))
	for _, r := range order {
		if n := seen[r]; n > 1 {
			parts = append(parts, fmt.Sprintf("`%s` \u00d7%d", r, n))
		} else {
			parts = append(parts, fmt.Sprintf("`%s`", r))
		}
	}
	return strings.Join(parts, ", ")
}

type generator struct {
	ctx  context.Context
	root string
	log  *log.Logger
}

func (g *generator) lint() (string, error) {
	g.log.Println("linting", workflow)
	out, err := g.command("go", "run", "./cmd/actionlint", "-no-color",
		"-config-file", filepath.Join(fixtureDir, config),
		filepath.Join(fixtureDir, workflow))
	if err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "", fmt.Errorf("%s reported nothing, so the demo section would show no problem", workflow)
	}
	// The path is reported the way it was passed, and the section shows it relative to the fixture.
	return strings.ReplaceAll(string(out), fixtureDir+string(filepath.Separator), ""), nil
}

func (g *generator) read(name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(g.root, fixtureDir, name))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\n"), nil
}

func (g *generator) section() (string, error) {
	wf, err := g.read(workflow)
	if err != nil {
		return "", err
	}
	cfg, err := g.read(config)
	if err != nil {
		return "", err
	}
	out, err := g.lint()
	if err != nil {
		return "", err
	}

	up, version, err := g.measureUpstream()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", beginMarker)
	fmt.Fprintf(&b, "`%s/%s`:\n\n```yaml\n%s\n```\n\n", fixtureDir, workflow, wf)
	fmt.Fprintf(&b, "`%s/%s`:\n\n```yaml\n%s\n```\n\n", fixtureDir, config, cfg)
	fmt.Fprintf(&b, "**Upstream actionlint %s reports %d: %s**\n\n", version, len(diagnostic.FindAllString(up, -1)), summarise(up))
	fmt.Fprintf(&b, "```console\n%s```\n\n", up)
	fmt.Fprintf(&b, "**This fork reports %d: %s**\n\n", len(diagnostic.FindAllString(out, -1)), summarise(out))
	fmt.Fprintf(&b, "```console\n%s```\n\n%s", out, endMarker)
	return b.String(), nil
}

func (g *generator) command(name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(g.ctx, name, args...)
	cmd.Dir = g.root
	cmd.Env = append(os.Environ(),
		"MISE_YES=1",
		"MISE_SILENT=1",
		"MISE_SAFE=1",
		"MISE_TRUSTED_CONFIG_PATHS="+g.root,
	)
	out, err := cmd.Output()
	if err != nil {
		var e *exec.ExitError
		if errors.As(err, &e) && e.ExitCode() == 1 {
			return out, nil // actionlint exits 1 when it reports problems
		}
		return nil, fmt.Errorf("could not run `%s`: %w", strings.Join(append([]string{name}, args...), " "), err)
	}
	return out, nil
}

func (g *generator) mise(args ...string) ([]byte, error) {
	return g.command("mise", args...)
}

// measureUpstream reports what the latest upstream command makes of the fixture.
func (g *generator) measureUpstream() (string, string, error) {
	v, err := g.mise("exec", upstreamTool, "--", "actionlint", "-version")
	if err != nil {
		return "", "", err
	}
	version, _, _ := strings.Cut(strings.TrimSpace(string(v)), "\n")
	if version == "" {
		return "", "", errors.New("mise reported no upstream version")
	}
	g.log.Println("measuring upstream", version)

	out, err := g.mise("exec", upstreamTool, "--", "actionlint", "-no-color",
		filepath.Join(fixtureDir, workflow))
	if err != nil {
		return "", "", err
	}
	// The path is reported the way it was passed, and the section shows it relative to the fixture.
	return strings.ReplaceAll(string(out), fixtureDir+string(filepath.Separator), ""), version, nil
}

func replace(doc, section string) (string, error) {
	start := strings.Index(doc, beginMarker)
	if start < 0 {
		return "", fmt.Errorf("the document has no %s marker", beginMarker)
	}
	end := strings.Index(doc, endMarker)
	if end < 0 {
		return "", fmt.Errorf("the document has no %s marker", endMarker)
	}
	if end < start {
		return "", fmt.Errorf("the %s marker comes before %s", endMarker, beginMarker)
	}
	return doc[:start] + section + doc[end+len(endMarker):], nil
}

func Main(ctx context.Context, args []string, stderr io.Writer) error {
	var fix, quiet bool
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.BoolVar(&fix, "fix", false, "rewrite the outdated section automatically")
	flags.BoolVar(&quiet, "quiet", false, "disable trace logs")
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: check-readme [FLAGS] FILE\n\nFlags:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("this command should take exact one file path but got %v", flags.Args())
	}

	out := io.Discard
	if !quiet {
		out = stderr
	}
	path := flags.Arg(0)
	g := &generator{ctx: ctx, root: filepath.Dir(path), log: log.New(out, "check-readme: ", log.LstdFlags)}
	if g.root == "" {
		g.root = "."
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("could not read the document file: %w", err)
	}
	section, err := g.section()
	if err != nil {
		return err
	}
	updated, err := replace(string(b), section)
	if err != nil {
		return err
	}

	if updated == string(b) {
		g.log.Println("the demo section is up-to-date")
		return nil
	}
	if !fix {
		return fmt.Errorf("the demo section is outdated. run `go run ./scripts/check-readme -fix %s` and commit the changes. the diff:\n\n%s", path, cmp.Diff(string(b), updated))
	}
	if err := os.WriteFile(path, []byte(updated), 0666); err != nil {
		return fmt.Errorf("could not write the document file: %w", err)
	}
	g.log.Println("wrote the demo section to", path)
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := Main(ctx, os.Args, os.Stderr)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
