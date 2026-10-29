package remotescan

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type snapshotError struct{ reason string }

func (e *snapshotError) Error() string   { return e.reason }
func unsafeSnapshot(reason string) error { return &snapshotError{reason} }

func (s *scanner) request(ctx context.Context, rawURL string, authenticated bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("invalid request URL")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "actionlint-scan")
	if authenticated && s.opts.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.opts.Token)
	}
	client := *s.client
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("redirect limit exceeded")
		}
		if next.URL.Scheme != "https" || next.URL.User != nil {
			return errors.New("unsafe redirect")
		}
		// Even sibling domains and redirects back to the origin do not inherit credentials.
		for _, previous := range via {
			if !strings.EqualFold(next.URL.Host, previous.URL.Host) {
				next.Header.Del("Authorization")
			}
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("HTTP request failed")
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return response, nil
}

func (s *scanner) resolveAPI(ctx context.Context) (string, error) {
	ref := s.opts.Ref
	if ref == "" {
		ref = "HEAD"
	}
	response, err := s.request(ctx, strings.TrimRight(s.opts.APIURL, "/")+"/repos/"+s.opts.Repository+"/commits/"+url.PathEscape(ref), true)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return "", errors.New("could not read commit response")
	}
	if len(data) > 1<<20 {
		return "", unsafeSnapshot("commit response exceeds limit")
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(data, &commit); err != nil || !shaPattern.MatchString(commit.SHA) {
		return "", unsafeSnapshot("API returned an invalid commit SHA")
	}
	if shaPattern.MatchString(s.opts.Ref) && !strings.EqualFold(commit.SHA, s.opts.Ref) {
		return "", unsafeSnapshot("API returned a different commit")
	}
	return strings.ToLower(commit.SHA), nil
}

type archiveEndpoint struct {
	name, url     string
	authenticated bool
}

func (s *scanner) acquire(ctx context.Context) (string, error) {
	sha := s.opts.Ref
	if shaPattern.MatchString(sha) {
		sha = strings.ToLower(sha)
	} else {
		sha = ""
	}
	local := ""
	if s.opts.Local != "" {
		if s.matchesLocal(ctx) {
			local = s.opts.Local
			if sha == "" {
				resolved, err := s.resolveLocal(ctx, local, s.opts.Ref)
				s.attempt("local-resolve", err)
				if err == nil {
					sha = resolved
				}
			}
		} else {
			s.attempt("local", errors.New("checkout origin does not match repository, or Git unavailable"))
		}
	}
	gitDir := ""
	if sha == "" {
		resolved, err := s.resolveAPI(ctx)
		s.attempt("api-resolve", err)
		if err == nil {
			sha = resolved
		} else {
			var invalid *snapshotError
			if errors.As(err, &invalid) {
				return "", err
			}
		}
	}
	if sha == "" {
		var err error
		gitDir, sha, err = s.fetchFallback(ctx, s.opts.Ref)
		if err != nil {
			return "", err
		}
	}
	s.result.Source.SHA = sha
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if local != "" {
		root, err := s.materializeGit(ctx, local, sha)
		s.attempt("local", err)
		if err == nil {
			s.result.Source.Transport = "local"
			return root, nil
		}
		var invalid *snapshotError
		if errors.As(err, &invalid) {
			return "", err
		}
	}
	if gitDir != "" {
		return s.fromFetchedGit(ctx, gitDir, sha)
	}
	server := strings.TrimRight(s.opts.ServerURL, "/")
	endpoints := []archiveEndpoint{{"api-archive", strings.TrimRight(s.opts.APIURL, "/") + "/repos/" + s.opts.Repository + "/tarball/" + sha, true}}
	if server == "https://github.com" {
		endpoints = append(endpoints, archiveEndpoint{"codeload", "https://codeload.github.com/" + s.opts.Repository + "/tar.gz/" + sha, false})
	}
	endpoints = append(endpoints, archiveEndpoint{"server-archive", server + "/" + s.opts.Repository + "/archive/" + sha + ".tar.gz", true})
	for _, endpoint := range endpoints {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		root, err := s.download(ctx, endpoint)
		s.attempt(endpoint.name, err)
		if err == nil {
			s.result.Source.Transport = endpoint.name
			return root, nil
		}
		var invalid *snapshotError
		if errors.As(err, &invalid) {
			return "", err
		}
	}
	gitDir, fetchedSHA, err := s.fetchFallback(ctx, sha)
	if err != nil {
		return "", err
	}
	if fetchedSHA != sha {
		return "", unsafeSnapshot("Git fetched a different commit")
	}
	return s.fromFetchedGit(ctx, gitDir, sha)
}

func (s *scanner) fromFetchedGit(ctx context.Context, dir, sha string) (string, error) {
	root, err := s.materializeGit(ctx, dir, sha)
	s.attempt("git-snapshot", err)
	return root, err
}

func (s *scanner) download(ctx context.Context, endpoint archiveEndpoint) (string, error) {
	response, err := s.request(ctx, endpoint.url, endpoint.authenticated)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	compressed := &io.LimitedReader{R: contextReader{ctx, response.Body}, N: s.opts.MaxBytes + 1}
	zipped, err := gzip.NewReader(compressed)
	if err != nil {
		return "", unsafeSnapshot("invalid gzip archive")
	}
	defer zipped.Close()
	root, err := os.MkdirTemp(s.temp, "snapshot-")
	if err != nil {
		return "", err
	}
	if err := extract(ctx, zipped, root, s.opts); err != nil {
		return "", err
	}
	if compressed.N <= 0 {
		return "", unsafeSnapshot("compressed archive exceeds limit")
	}
	return root, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// safePath enforces portable names, including Windows drive/ADS/device and case-fold collisions.
func safePath(name string) bool {
	if name == "" || len(name) > 4096 || strings.ContainsAny(name, "\\:\x00") || path.Clean(name) != name || !filepath.IsLocal(filepath.FromSlash(name)) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." || part == "" || strings.EqualFold(part, ".git") || strings.TrimRight(part, ". ") != part {
			return false
		}
		for _, r := range part {
			if r < 32 || r == 127 {
				return false
			}
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
			return false
		}
	}
	return true
}

type snapshotWriter struct {
	root  string
	opts  Options
	bytes int64
	files int
	names map[string]string
}

func (w *snapshotWriter) write(ctx context.Context, name string, size int64, source io.Reader) error {
	if !safePath(name) {
		return unsafeSnapshot("unsafe snapshot path")
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		key := strings.ToLower(prefix)
		if previous, ok := w.names[key]; ok && previous != prefix {
			return unsafeSnapshot("case-colliding snapshot paths")
		}
		w.names[key] = prefix
	}
	w.files++
	if w.files > w.opts.MaxFiles || size < 0 || size > w.opts.MaxFileBytes || size > w.opts.MaxBytes-w.bytes {
		return unsafeSnapshot("snapshot exceeds file or size limit")
	}
	w.bytes += size
	dest := filepath.Join(w.root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return unsafeSnapshot("conflicting snapshot paths")
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return unsafeSnapshot("conflicting snapshot paths")
	}
	_, copyErr := io.CopyN(f, contextReader{ctx, source}, size)
	closeErr := f.Close()
	if copyErr != nil {
		return unsafeSnapshot("truncated or cancelled snapshot")
	}
	return closeErr
}

func extract(ctx context.Context, input io.Reader, root string, opts Options) error {
	// Include tar headers/padding and gzip trailing data in a separate expansion bound.
	bounded := &io.LimitedReader{R: contextReader{ctx, input}, N: opts.MaxBytes + int64(opts.MaxFiles)*1024 + 1}
	reader := tar.NewReader(bounded)
	writer := snapshotWriter{root: root, opts: opts, names: map[string]string{}}
	prefix := ""
	entries := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return unsafeSnapshot("invalid or truncated tar archive")
		}
		entries++
		if entries > opts.MaxFiles*2 {
			return unsafeSnapshot("archive entry limit exceeded")
		}
		name := strings.TrimSuffix(header.Name, "/")
		if !safePath(name) {
			return unsafeSnapshot("unsafe archive path")
		}
		parts := strings.SplitN(name, "/", 2)
		if prefix == "" {
			prefix = parts[0]
		}
		if prefix != parts[0] {
			return unsafeSnapshot("archive has multiple roots")
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg || len(parts) != 2 {
			return unsafeSnapshot("archive contains links or unsupported entries")
		}
		if err := writer.write(ctx, parts[1], header.Size, reader); err != nil {
			return err
		}
	}
	if _, err := io.Copy(io.Discard, bounded); err != nil {
		return unsafeSnapshot("invalid archive trailer")
	}
	if bounded.N <= 0 {
		return unsafeSnapshot("expanded archive exceeds limit")
	}
	if writer.files == 0 {
		return unsafeSnapshot("empty archive")
	}
	return nil
}
