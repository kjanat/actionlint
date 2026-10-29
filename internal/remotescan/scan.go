// Package remotescan scans immutable GitHub repository snapshots without checking out code.
package remotescan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"actionlint.kjanat.dev"
)

// Options selects a repository and bounds network and filesystem work. Token is never serialized.
type Options struct {
	Repository   string
	Ref          string
	ServerURL    string
	APIURL       string
	Local        string
	Token        string
	SSHURL       string
	ConfigFile   string
	Timeout      time.Duration
	MaxBytes     int64
	MaxFileBytes int64
	MaxFiles     int
}

// Source identifies the exact commit scanned, including the successful acquisition transport.
type Source struct {
	Repository   string `json:"repository"`
	RequestedRef string `json:"requested_ref"`
	SHA          string `json:"sha"`
	ServerURL    string `json:"server_url"`
	Transport    string `json:"transport"`
}

// Diagnostic is an actionlint finding with an immutable source link.
type Diagnostic struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	URL     string `json:"url"`
}

// Result is the versioned JSON result, also produced on acquisition or lint failures.
type Result struct {
	Version     int          `json:"version"`
	Source      Source       `json:"source"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Attempts    []string     `json:"attempts"`
	Error       string       `json:"error,omitempty"`
}

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// Defaults applies Actions environment defaults only for the selected repository. Explicit refs win.
// GITHUB_GRAPHQL_URL is deliberately unused: this scanner uses REST and does not need GraphQL.
func Defaults(opts Options, getenv func(string) string) Options {
	if opts.Repository == "" {
		opts.Repository = getenv("GITHUB_REPOSITORY")
	}
	if opts.ServerURL == "" {
		opts.ServerURL = getenv("GITHUB_SERVER_URL")
	}
	if opts.ServerURL == "" {
		opts.ServerURL = "https://github.com"
	}
	opts.ServerURL = strings.TrimRight(opts.ServerURL, "/")
	if strings.EqualFold(opts.ServerURL, "https://github.com") {
		opts.ServerURL = "https://github.com"
	}
	actionsServer := strings.TrimRight(getenv("GITHUB_SERVER_URL"), "/")
	if actionsServer == "" {
		actionsServer = "https://github.com"
	}
	if opts.Ref == "" && opts.Repository == getenv("GITHUB_REPOSITORY") && strings.EqualFold(opts.ServerURL, actionsServer) {
		opts.Ref = getenv("GITHUB_SHA")
		if opts.Ref == "" {
			opts.Ref = getenv("GITHUB_REF")
		}
	}
	if opts.APIURL == "" && strings.EqualFold(opts.ServerURL, actionsServer) {
		opts.APIURL = getenv("GITHUB_API_URL")
	}
	if opts.APIURL == "" {
		if opts.ServerURL == "https://github.com" {
			opts.APIURL = "https://api.github.com"
		} else {
			opts.APIURL = strings.TrimRight(opts.ServerURL, "/") + "/api/v3"
		}
	}
	opts.APIURL = strings.TrimRight(opts.APIURL, "/")
	if opts.Token == "" {
		opts.Token = getenv("GH_TOKEN")
	}
	if opts.Token == "" {
		opts.Token = getenv("GITHUB_TOKEN")
	}
	if opts.Local == "" {
		opts.Local = getenv("GITHUB_WORKSPACE")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 2 * time.Minute
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = 256 << 20
	}
	if opts.MaxFileBytes == 0 {
		opts.MaxFileBytes = 16 << 20
	}
	if opts.MaxFiles == 0 {
		opts.MaxFiles = 20000
	}
	return opts
}

func baseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("server and API URLs must be HTTPS URLs without credentials, queries or fragments")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

// Validate rejects malformed targets and unsafe or unbounded resource options.
func Validate(opts Options) error {
	if !repositoryPattern.MatchString(opts.Repository) {
		return errors.New("repository must be OWNER/REPO")
	}
	for _, part := range strings.Split(opts.Repository, "/") {
		if part == "." || part == ".." {
			return errors.New("invalid repository name")
		}
	}
	if strings.HasPrefix(opts.Ref, "-") || strings.ContainsAny(opts.Ref, "\x00\r\n\t ~^:?*[\\") || strings.Contains(opts.Ref, "..") || strings.Contains(opts.Ref, "@{") {
		return errors.New("invalid Git ref")
	}
	if opts.Timeout <= 0 || opts.MaxBytes <= 0 || opts.MaxFileBytes <= 0 || opts.MaxFileBytes > opts.MaxBytes || opts.MaxFiles <= 0 {
		return errors.New("timeout and snapshot limits must be positive; file limit must not exceed total limit")
	}
	if opts.MaxBytes > 4<<30 || opts.MaxFiles > 1000000 {
		return errors.New("snapshot limits must not exceed 4 GiB or 1000000 files")
	}
	if strings.ContainsAny(opts.Token, "\r\n\x00") {
		return errors.New("invalid token")
	}
	if _, err := baseURL(opts.ServerURL); err != nil {
		return err
	}
	if _, err := baseURL(opts.APIURL); err != nil {
		return err
	}
	if opts.SSHURL != "" {
		u, err := url.Parse(opts.SSHURL)
		if err != nil || u.Scheme != "ssh" || u.Host == "" || strings.HasPrefix(u.Host, "-") || u.User == nil || u.User.String() != "git" || u.RawQuery != "" || u.Fragment != "" || u.Path != "/"+opts.Repository+".git" {
			return errors.New("SSH URL must be ssh://git@HOST[:PORT]/OWNER/REPO.git for the selected repository")
		}
	}
	return nil
}

type scanner struct {
	opts   Options
	client *http.Client
	result Result
	temp   string
}

// Scan acquires a bounded snapshot, runs actionlint with explicit project context, and removes it.
// External analyzers are disabled. ConfigFile, when set, is a trusted caller-supplied policy override.
func Scan(ctx context.Context, opts Options) (Result, error) {
	return scan(ctx, opts, &http.Client{Timeout: 30 * time.Second})
}

func scan(ctx context.Context, opts Options, client *http.Client) (result Result, returned error) {
	result = Result{Version: 1, Source: Source{Repository: opts.Repository, RequestedRef: opts.Ref, ServerURL: opts.ServerURL}, Diagnostics: []Diagnostic{}, Attempts: []string{}}
	defer func() {
		if returned != nil {
			result.Error = returned.Error()
		}
	}()
	if err := Validate(opts); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	temp, err := os.MkdirTemp("", "actionlint-scan-")
	if err != nil {
		return result, err
	}
	defer func() {
		if err := os.RemoveAll(temp); err != nil {
			returned = errors.Join(returned, errors.New("could not remove scanner temporary directory"))
		}
	}()
	s := scanner{opts: opts, client: client, temp: temp, result: result}
	root, err := s.acquire(ctx)
	result = s.result
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	project, err := actionlint.NewProject(root)
	if err != nil {
		return result, err
	}
	linter, err := actionlint.NewLinter(io.Discard, &actionlint.LinterOptions{WorkingDir: root, Context: ctx, ConfigFile: opts.ConfigFile})
	if err != nil {
		return result, err
	}
	findings, err := linter.LintDir(project.WorkflowsDir(), project)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for _, finding := range findings {
		path := finding.Filepath
		if filepath.IsAbs(path) {
			path, err = filepath.Rel(root, path)
			if err != nil {
				return result, err
			}
		}
		path = filepath.ToSlash(path)
		link := strings.TrimRight(opts.ServerURL, "/") + "/" + opts.Repository + "/blob/" + result.Source.SHA + "/" + escapePath(path) + fmt.Sprintf("#L%d", finding.Line)
		message := strings.ReplaceAll(finding.Message, root, ".")
		result.Diagnostics = append(result.Diagnostics, Diagnostic{path, finding.Line, finding.Column, finding.Kind, message, link})
	}
	return result, nil
}

func escapePath(path string) string {
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func (s *scanner) attempt(name string, err error) {
	status := "ok"
	if err != nil {
		status = err.Error()
	}
	s.result.Attempts = append(s.result.Attempts, name+": "+status)
}

// WriteJSON writes one complete machine-readable report.
func WriteJSON(w io.Writer, result Result) error { return json.NewEncoder(w).Encode(result) }
