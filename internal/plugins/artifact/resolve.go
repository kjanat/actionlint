package artifact

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"actionlint.kjanat.dev/internal/fsread"
)

// Options bounds artifact acquisition independently of runtime execution limits.
type Options struct {
	// CacheDir stores verified downloads pinned by SHA256. Empty disables caching.
	CacheDir string
	// Timeout defaults to 30 seconds, including redirects and response reading.
	Timeout time.Duration
	// MaxBytes bounds both distributed and decompressed bytes; defaults to 128 MiB.
	MaxBytes int64
}

// Resolve accepts paths, absolute file URIs and HTTP(S) URLs. A URI fragment of
// #sha256=<hex> pins the distributed bytes. Unpinned URLs are always fetched.
func Resolve(ctx context.Context, source string, options Options) (*Artifact, error) {
	if options.Timeout <= 0 {
		options.Timeout = 30 * time.Second
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = 128 << 20
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	u, local, pin, err := parseSource(source)
	if err != nil {
		return nil, err
	}
	display := source
	if u != nil {
		redacted := *u
		redacted.RawQuery, redacted.Fragment, redacted.RawFragment = "", "", ""
		display = redacted.String()
	}
	var data []byte
	if local != "" {
		data, err = readFile(ctx, local, options.MaxBytes)
	} else {
		data, err = download(ctx, u, pin, options)
	}
	if err != nil {
		return nil, fmt.Errorf("plugin artifact %s: %w", display, err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if pin != "" && pin != digest {
		return nil, fmt.Errorf("plugin artifact %s: SHA256 mismatch: expected %s, got %s", display, pin, digest)
	}
	info := Info{Source: display, SHA256: digest, Size: len(data)}
	if bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("plugin gzip: %w", err)
		}
		data, err = readBounded(gz, options.MaxBytes)
		_ = gz.Close()
		if err != nil {
			return nil, fmt.Errorf("plugin gzip: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info.Kind, info.Metadata, err = Sniff(data)
	if err != nil {
		return nil, fmt.Errorf("plugin artifact %s: %w", display, err)
	}
	info.ModuleSize = len(data)
	return &Artifact{Info: info, Bytes: data}, nil
}

func parseSource(source string) (*url.URL, string, string, error) {
	// Drive paths (including forward slashes) are paths on every host. Only
	// explicit :// locations are URIs, preserving colons in ordinary filenames.
	if !strings.Contains(source, "://") {
		if source == "" {
			return nil, "", "", errors.New("empty plugin location")
		}
		return nil, source, "", nil
	}
	u, err := url.Parse(source)
	if err != nil {
		return nil, "", "", errors.New("invalid plugin URI")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.User != nil {
		return nil, "", "", errors.New("plugin URIs cannot contain credentials")
	}
	pin := ""
	if u.Fragment != "" {
		pin = strings.TrimPrefix(u.Fragment, "sha256=")
		decoded, err := hex.DecodeString(pin)
		if !strings.HasPrefix(u.Fragment, "sha256=") || err != nil || len(decoded) != sha256.Size {
			return nil, "", "", errors.New("plugin URI fragment must be sha256=<64 hex digits>")
		}
		pin = strings.ToLower(pin)
	}
	u.Fragment, u.RawFragment = "", ""
	switch u.Scheme {
	case "http", "https":
		if u.Host == "" {
			return nil, "", "", errors.New("plugin URL requires a host")
		}
		return u, "", pin, nil
	case "file":
		if (u.Host != "" && !strings.EqualFold(u.Host, "localhost")) || u.RawQuery != "" {
			return nil, "", "", errors.New("file plugin URI requires a local absolute path without a query")
		}
		path := u.Path
		if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
		path = filepath.FromSlash(path)
		if !filepath.IsAbs(path) {
			return nil, "", "", errors.New("file plugin URI requires an absolute path")
		}
		return u, path, pin, nil
	default:
		return nil, "", "", errors.New("plugin URI scheme must be file, http or https")
	}
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, err
	}
	var extra [1]byte
	n, err := io.ReadFull(r, extra[:])
	if n != 0 {
		return nil, fmt.Errorf("artifact exceeds %d byte limit", limit)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return data, nil
}

func readFile(ctx context.Context, path string, limit int64) ([]byte, error) {
	f, err := fsread.New(ctx, nil, nil).Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stop := context.AfterFunc(ctx, func() { _ = f.Close() })
	defer stop()
	info, err := f.Stat()
	if err != nil {
		return nil, errors.Join(err, ctx.Err())
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("artifact exceeds %d byte limit", limit)
	}
	data, err := readBounded(f, limit)
	if err = errors.Join(err, ctx.Err()); err != nil {
		return nil, err
	}
	return data, nil
}

func download(ctx context.Context, u *url.URL, pin string, options Options) ([]byte, error) {
	cachePath := ""
	if pin != "" && options.CacheDir != "" {
		cachePath = filepath.Join(options.CacheDir, pin)
		data, err := readFile(ctx, cachePath, options.MaxBytes)
		if err == nil {
			if fmt.Sprintf("%x", sha256.Sum256(data)) != pin {
				return nil, errors.New("cached plugin artifact SHA256 mismatch")
			}
			return data, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many plugin redirects")
		}
		if req.URL.User != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
			return errors.New("invalid plugin redirect")
		}
		if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("plugin redirect downgrades HTTPS")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("invalid plugin request")
	}
	// Verify checksums against the published bytes by disabling HTTP decoding.
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		// Transport errors may still embed a signed redirect URL. Preserve context
		// errors but keep arbitrary transport details out of diagnostics.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("plugin HTTP request failed (connection or redirect rejected)")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plugin server returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > options.MaxBytes {
		return nil, fmt.Errorf("artifact exceeds %d byte limit", options.MaxBytes)
	}
	data, err := readBounded(resp.Body, options.MaxBytes)
	if err != nil {
		return nil, err
	}
	if pin != "" && fmt.Sprintf("%x", sha256.Sum256(data)) != pin {
		return nil, errors.New("downloaded plugin artifact SHA256 mismatch")
	}
	if cachePath != "" {
		if err := cacheFile(ctx, cachePath, data); err != nil {
			return nil, fmt.Errorf("plugin artifact cache: %w", err)
		}
	}
	return data, nil
}

func cacheFile(ctx context.Context, path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		// Another process may have published the same pinned artifact on a platform
		// where rename cannot replace an existing file. Accept only identical bytes.
		existing, readErr := readFile(ctx, path, int64(len(data)))
		if readErr == nil && bytes.Equal(existing, data) {
			return nil
		}
		return err
	}
	return nil
}
