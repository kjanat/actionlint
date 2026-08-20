package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const releaseJobURL = "https://github.com/kjanat/actionlint/actions/workflows/release.yaml"

type repo struct {
	root string
	out  io.Writer
}

func (r *repo) git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("`git %s` failed: %s: %w", strings.Join(args, " "), msg, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (r *repo) run(args ...string) error {
	fmt.Fprintf(r.out, "+ git %s\n", strings.Join(args, " "))
	_, err := r.git(args...)
	return err
}

func (r *repo) preflight(tag string) error {
	top, err := r.git("rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(r.root)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	topResolved, err := filepath.EvalSymlinks(top)
	if err != nil {
		return err
	}
	if resolved != topResolved {
		return fmt.Errorf("this command must run at the repository root but %q is not %q", resolved, topResolved)
	}

	if out, err := r.git("status", "--porcelain", "--untracked-files=all"); err != nil {
		return err
	} else if out != "" {
		return errors.New("the working tree is not clean. commit, stash, or remove all changed and untracked files before bumping the version")
	}

	branch, err := r.git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if branch != "main" {
		return fmt.Errorf("this command must run on the 'main' branch but the current branch is %q", branch)
	}

	if _, err := r.git("rev-parse", "--verify", "--quiet", "refs/tags/"+tag); err == nil {
		return fmt.Errorf("tag %s already exists", tag)
	}
	out, err := r.git("ls-remote", "--tags", "origin", "refs/tags/"+tag)
	if err != nil {
		return fmt.Errorf("could not check origin for tag %s: %w", tag, err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("tag %s already exists on origin", tag)
	}
	return nil
}

func touch(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return fmt.Errorf("could not update %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("could not update %s: %w", path, err)
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		return fmt.Errorf("could not update %s: %w", path, err)
	}
	return nil
}

func Main(args []string, stdout, stderr io.Writer) error {
	var check, commit, push bool
	var root string

	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.BoolVar(&check, "check", false, "verify the declared version references and exit without modifying anything")
	flags.BoolVar(&commit, "commit", false, "create the version bump commit and the version tag after verification")
	flags.BoolVar(&push, "push", false, "push the version bump commit and the version tag, implies -commit")
	flags.StringVar(&root, "root", ".", "repository root directory")
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: bump-version [FLAGS] VERSION\n\nFlags:")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if check {
		if flags.NArg() != 0 {
			return fmt.Errorf("-check takes no argument but got %v", flags.Args())
		}
		return Check(root, targets, stdout)
	}

	if flags.NArg() != 1 {
		return fmt.Errorf("this command takes exactly one version argument such as 1.2.3 but got %v", flags.Args())
	}
	v, err := parseVersion(flags.Arg(0))
	if err != nil {
		return err
	}
	tag := "v" + v.String()

	r := &repo{root: root, out: stdout}
	if err := r.preflight(tag); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Bumping the version to %s (tag: %s)\n", v, tag)
	if err := Bump(root, targets, v, stdout); err != nil {
		return err
	}

	if err := touch(filepath.Join(root, ".bumptimestamp")); err != nil {
		return err
	}

	if !commit && !push {
		fmt.Fprint(stdout, "\nAll version references were updated and verified. To release, run:\n\n")
		fmt.Fprintf(stdout, "  git add %s\n", strings.Join(paths(targets), " "))
		fmt.Fprintf(stdout, "  git commit -m 'bump up version to %s'\n", tag)
		fmt.Fprintf(stdout, "  git tag %s\n", tag)
		fmt.Fprint(stdout, "  git push origin main\n")
		fmt.Fprintf(stdout, "  git push origin %s\n", tag)
		return nil
	}

	if err := r.run(append([]string{"add"}, paths(targets)...)...); err != nil {
		return err
	}
	if err := r.run("commit", "-m", "bump up version to "+tag); err != nil {
		return err
	}
	if err := r.run("tag", tag); err != nil {
		return err
	}

	if !push {
		fmt.Fprintf(stdout, "\nThe bump commit and the tag %s were created locally. To release, run:\n\n", tag)
		fmt.Fprint(stdout, "  git push origin main\n")
		fmt.Fprintf(stdout, "  git push origin %s\n", tag)
		return nil
	}

	// docker/build-push-action resolves the tagged commit through the main branch, so main must be pushed first.
	if err := r.run("push", "origin", "main"); err != nil {
		return err
	}
	if err := r.run("push", "origin", tag); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "\nCheck the release progress at %s\n", releaseJobURL)
	return nil
}

func main() {
	if err := Main(os.Args, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
