package artifacts

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"time"

	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp"
	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp/accounting"
	"actionlint.kjanat.dev/v2/internal/integrations/network"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
)

// Options bounds artifact acquisition independently of runtime execution limits.
type Options struct {
	// Network grants private access only to an invocation-selected authority.
	// The zero value confines automatic acquisition to public destinations.
	Network     acquisitionhttp.Grant
	Connections acquisitionhttp.Connections
	// CacheDir stores verified downloads pinned by SHA256. Empty disables caching.
	CacheDir string
	// Timeout defaults to 30 seconds, including redirects and response reading.
	Timeout time.Duration
	// MaxBytes bounds both distributed and decompressed bytes; defaults to 128 MiB.
	MaxBytes int64
	// PullOCI pulls oci:// sources; the oci subpackage's Pull is the
	// implementation. Nil refuses oci:// sources.
	PullOCI PullOCI
}

// RequireRemotePin checks execution inputs without fetching their bytes.
// Inspection may acquire unpinned URLs to discover their digest; execution may not.
func RequireRemotePin(source string) error {
	if pluginreference.IsOCI(source) {
		_, err := pluginreference.ParseOCI(source)
		return err
	}
	location, err := pluginreference.ParseModuleLocation(source)
	if err != nil {
		return err
	}
	if location.Endpoint != nil && location.Local == "" && location.Pin == "" {
		return errors.New("remote plugin execution requires #sha256=<64 hex digits>")
	}
	return nil
}

// Default acquisition limits apply when Timeout or MaxBytes is not positive.
const (
	DefaultTimeout  = 30 * time.Second
	DefaultMaxBytes = 128 << 20
)

// withDefaults returns options with the default Timeout and MaxBytes set.
func (o Options) withDefaults() Options {
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	return o
}

// Resolve accepts paths, absolute file URIs, HTTP(S) URLs and oci:// digest
// references, which Options.PullOCI pulls. A URI fragment of #sha256=<hex>
// pins the distributed bytes. Unpinned URLs are always fetched.
// Options.Timeout bounds the whole pull.
func Resolve(ctx context.Context, source string, options Options) (*Artifact, error) {
	options = options.withDefaults()
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	if pluginreference.IsOCI(source) {
		ref, err := pluginreference.ParseOCI(source)
		if err != nil {
			return nil, err
		}
		if options.PullOCI == nil {
			return nil, fmt.Errorf("plugin artifact %s: this caller cannot pull oci:// sources", ref)
		}
		artifact, err := options.PullOCI(ctx, ref, options)
		if err != nil {
			return nil, fmt.Errorf("plugin artifact %s: %w", ref, err)
		}
		if artifact == nil {
			return nil, fmt.Errorf("plugin artifact %s: OCI pull returned no artifact", ref)
		}
		snapshot := *artifact
		snapshot.Bytes = bytes.Clone(artifact.Bytes)
		snapshot.Info.Metadata = bytes.Clone(artifact.Info.Metadata)
		snapshot.Info.Blobs = slices.Clone(artifact.Info.Blobs)
		return &snapshot, nil
	}
	location, err := pluginreference.ParseModuleLocation(source)
	if err != nil {
		return nil, err
	}
	u, local, pin := location.Endpoint, location.Local, location.Pin
	display := source
	if u != nil {
		redacted := *u
		redacted.RawQuery, redacted.Fragment, redacted.RawFragment = "", "", ""
		display = redacted.String()
	}
	var data []byte
	switch {
	case u != nil && u.Scheme == "sha256":
		if options.CacheDir == "" {
			return nil, errors.New("candidate execution requires an artifact cache")
		}
		data, err = readFile(ctx, filepath.Join(options.CacheDir, pin), options.MaxBytes)
	case local != "":
		data, err = readFile(ctx, local, options.MaxBytes)
	default:
		data, err = download(ctx, u, pin, options)
	}
	if err != nil {
		return nil, fmt.Errorf("plugin artifact %s: %w", display, err)
	}
	digest := sha256hex.Digest(data)
	if pin != "" && pin != digest {
		return nil, fmt.Errorf("plugin artifact %s: SHA256 mismatch: expected %s, got %s", display, pin, digest)
	}
	info := ArtifactProvenance{Source: display, SHA256: digest, Size: len(data)}
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
	info.Kind, info.Metadata, err = SniffPackage(data)
	if err != nil {
		return nil, fmt.Errorf("plugin artifact %s: %w", display, err)
	}
	info.ModuleSize = len(data)
	return &Artifact{Info: info, Bytes: data}, nil
}

// Import verifies candidate bytes before adding them to the download cache.
// Callers open local inputs within their selected catalogue root.
func Import(ctx context.Context, input io.Reader, digest string, options Options) error {
	if pluginreference.CheckDigest(digest) != nil || options.CacheDir == "" {
		return errors.New("candidate import requires a digest and cache directory")
	}
	limit := options.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	data, err := readBounded(input, limit)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if sha256hex.Digest(data) != digest {
		return errors.New("candidate artifact SHA256 mismatch")
	}
	return cacheFile(ctx, filepath.Join(options.CacheDir, digest), data)
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
	f, err := files.OpenShared(ctx, path)
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
			if sha256hex.Digest(data) != pin {
				return nil, errors.New("cached plugin artifact SHA256 mismatch")
			}
			return data, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if network.Settings(ctx).Offline {
		return nil, network.Missing(u.String())
	}
	transport, err := acquisitionhttp.Transport(u, options.Network, options.Connections)
	if err != nil {
		return nil, acquisitionError(err)
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
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
	ctx = accounting.WithRequestOptions(ctx, accounting.RequestOptions{Purpose: "plugin-artifact", Priority: accounting.Required})
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
		if _, known := acquisitionhttp.Classify(err); known {
			return nil, acquisitionError(err)
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
	if pin != "" && sha256hex.Digest(data) != pin {
		return nil, errors.New("downloaded plugin artifact SHA256 mismatch")
	}
	if cachePath == "" && options.CacheDir != "" {
		cachePath = filepath.Join(options.CacheDir, sha256hex.Digest(data))
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		// Concurrent writers can publish the same artifact. Accept identical bytes.
		existing, readErr := readFile(ctx, path, int64(len(data)))
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if readErr == nil && bytes.Equal(existing, data) {
			return nil
		}
		return errors.Join(err, readErr)
	}
	return nil
}
