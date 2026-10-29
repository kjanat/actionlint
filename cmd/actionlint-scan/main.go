// actionlint-scan checks an immutable GitHub repository snapshot without a checkout.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"actionlint.kjanat.dev/internal/remotescan"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

func run(ctx context.Context, args []string, out, stderr io.Writer, getenv func(string) string) int {
	var opts remotescan.Options
	flags := flag.NewFlagSet("actionlint-scan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&opts.Repository, "repository", "", "GitHub OWNER/REPO (defaults to GITHUB_REPOSITORY)")
	flags.StringVar(&opts.Ref, "ref", "", "commit SHA, branch or tag (overrides positional @ref and Actions defaults)")
	flags.StringVar(&opts.ServerURL, "server-url", "", "GitHub HTTPS server URL")
	flags.StringVar(&opts.APIURL, "api-url", "", "GitHub REST API base URL")
	flags.StringVar(&opts.Local, "local", "", "existing checkout used only when origin matches repository; committed contents only")
	flags.StringVar(&opts.SSHURL, "ssh-url", "", "SSH fallback URL: ssh://git@HOST[:PORT]/OWNER/REPO.git")
	flags.StringVar(&opts.ConfigFile, "config-file", "", "trusted local actionlint policy configuration override")
	flags.DurationVar(&opts.Timeout, "timeout", 0, "overall acquisition/lint timeout (default 2m)")
	flags.Int64Var(&opts.MaxBytes, "max-bytes", 0, "maximum total snapshot bytes (default 268435456)")
	flags.Int64Var(&opts.MaxFileBytes, "max-file-bytes", 0, "maximum bytes per file (default 16777216)")
	flags.IntVar(&opts.MaxFiles, "max-files", 0, "maximum snapshot files (default 20000)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: actionlint-scan [options] [OWNER/REPO[@REF]]\nJSON output; exit 0 clean, 1 findings, 2 invalid options, 3 failure.\nAuthentication: GH_TOKEN, then GITHUB_TOKEN. Git fallback requires git; SSH requires a configured key/agent.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() > 1 || (flags.NArg() == 1 && opts.Repository != "") {
		fmt.Fprintln(stderr, "specify one repository, positionally or with --repository")
		return 2
	}
	if flags.NArg() == 1 {
		repo, ref, hasRef := strings.Cut(flags.Arg(0), "@")
		if hasRef && ref == "" {
			fmt.Fprintln(stderr, "@ref must not be empty")
			return 2
		}
		opts.Repository = repo
		if opts.Ref == "" {
			opts.Ref = ref
		}
	}
	opts = remotescan.Defaults(opts, getenv)
	if err := remotescan.Validate(opts); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	result, err := remotescan.Scan(ctx, opts)
	if writeErr := remotescan.WriteJSON(out, result); writeErr != nil {
		fmt.Fprintln(stderr, "could not write JSON report")
		return 3
	}
	if err != nil {
		return 3
	}
	if len(result.Diagnostics) != 0 {
		return 1
	}
	return 0
}
