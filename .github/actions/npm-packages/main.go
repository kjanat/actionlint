// Command npm-packages assembles the npm publish tree for actionlint, and is
// run by the npm-packages composite action.
//
// actionlint ships to npm the way a native binary normally does: one facade
// package, @kjanat/actionlint, that carries no binary itself, plus one small
// package per platform holding a single executable. The facade declares every
// platform package in optionalDependencies with an exact version, and npm
// installs only the one whose "os" and "cpu" match the host. At run time the
// facade's bin shim resolves that package and execs the binary inside it.
//
// The binaries are not rebuilt here. They are lifted out of the GoReleaser
// archives already published on the GitHub release, and each archive is checked
// against the release's own checksums manifest before being unpacked, so what
// npm serves is byte-for-byte what the release attested.
//
// Unlike the same layout for a Rust binary there is no libc dimension: the
// release binaries are built with CGO_ENABLED=0 and are statically linked, so a
// single linux-<cpu> package covers glibc and musl hosts alike.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// maxArchive bounds memory use when reading a release asset.
const maxArchive = 256 << 20 // 256 MiB

var semverRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// target is one published platform, as declared in distribution/npm/targets.json.
type target struct {
	Pkg    string `json:"pkg"`    // npm name suffix, always "<os>-<cpu>"
	OS     string `json:"os"`     // Node process.platform value
	CPU    string `json:"cpu"`    // Node process.arch value
	Asset  string `json:"asset"`  // GOOS_GOARCH pair in the release asset name
	Format string `json:"format"` // tar.gz or zip
	Tier   int    `json:"tier"`
}

type targetsFile struct {
	Facade        string   `json:"facade"`
	PlatformScope string   `json:"platformScope"`
	Binary        string   `json:"binary"`
	Targets       []target `json:"targets"`
}

type config struct {
	version    string
	repository string
	token      string
	repoRoot   string
	npmDir     string
	outDir     string
	downloads  string
	only       string

	// manual is the roff man page from the first archive unpacked. Every
	// archive carries the same copy; the facade reuses this one.
	manual []byte
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "npm-packages: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	targets, err := loadTargets(filepath.Join(cfg.npmDir, "targets.json"))
	if err != nil {
		return err
	}
	selected, err := selectTargets(targets, cfg.only)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.outDir, 0o755); err != nil {
		return err
	}

	// Fetched once and shared by every target: one manifest per release.
	var sums map[string]string
	if len(selected) > 0 {
		if sums, err = cfg.checksums(); err != nil {
			return err
		}
	}

	for _, t := range selected {
		if err := cfg.buildPlatformPackage(targets, t, sums); err != nil {
			return fmt.Errorf("%s: %w", t.Pkg, err)
		}
	}

	if cfg.only == "" || cfg.only == "facade" {
		if err := cfg.buildFacade(targets); err != nil {
			return fmt.Errorf("facade: %w", err)
		}
	}
	return nil
}

func loadConfig() (*config, error) {
	workspace := os.Getenv("GITHUB_WORKSPACE")
	if workspace == "" {
		workspace = "."
	}
	cfg := &config{
		version:    strings.TrimPrefix(os.Getenv("INPUT_VERSION"), "v"),
		repository: os.Getenv("INPUT_REPOSITORY"),
		token:      os.Getenv("INPUT_TOKEN"),
		repoRoot:   workspace,
		npmDir:     os.Getenv("INPUT_NPM_DIR"),
		outDir:     os.Getenv("INPUT_OUT_DIR"),
		downloads:  os.Getenv("INPUT_DOWNLOADS"),
		only:       os.Getenv("INPUT_ONLY"),
	}
	if !semverRe.MatchString(cfg.version) {
		return nil, fmt.Errorf("version %q is not X.Y.Z or X.Y.Z-prerelease", cfg.version)
	}
	if cfg.npmDir == "" {
		cfg.npmDir = filepath.Join(workspace, "distribution", "npm")
	}
	if cfg.outDir == "" {
		cfg.outDir = filepath.Join(cfg.npmDir, "dist")
	}
	return cfg, nil
}

func loadTargets(path string) (*targetsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tf targetsFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if tf.Facade == "" || tf.PlatformScope == "" || tf.Binary == "" {
		return nil, fmt.Errorf("%s must set facade, platformScope and binary", path)
	}
	if !strings.HasPrefix(tf.PlatformScope, "@") || strings.Contains(tf.PlatformScope, "/") {
		return nil, fmt.Errorf("%s: platformScope %q must be a bare npm scope such as @kjanat-actionlint", path, tf.PlatformScope)
	}
	for _, t := range tf.Targets {
		// The resolver looks for <platformScope>/<binary>-<os>-<cpu>. A pkg
		// that disagrees names a package the resolver never finds.
		if want := t.OS + "-" + t.CPU; t.Pkg != want {
			return nil, fmt.Errorf("target %q must be named %q to match os and cpu", t.Pkg, want)
		}
		if t.Format != "tar.gz" && t.Format != "zip" {
			return nil, fmt.Errorf("target %q has unsupported format %q", t.Pkg, t.Format)
		}
	}
	return &tf, nil
}

// selectTargets narrows the target list to the one named by only. An empty only
// builds everything; "facade" builds no platform packages at all.
func selectTargets(tf *targetsFile, only string) ([]target, error) {
	switch only {
	case "":
		return tf.Targets, nil
	case "facade":
		return nil, nil
	}
	// Either the short form or the fully qualified npm name.
	for _, t := range tf.Targets {
		if only == t.Pkg || only == packageName(tf.PlatformScope, tf.Binary, t.Pkg) {
			return []target{t}, nil
		}
	}
	return nil, fmt.Errorf("no target matches %q", only)
}

// packageName builds the npm name of a platform package. The binary's name is
// kept in the package name: a lockfile or scanner shows the package name alone,
// and "linux-x64" says nothing about what it holds.
func packageName(scope, binary, pkg string) string { return scope + "/" + binary + "-" + pkg }

// assetName is the release asset holding a target's binary.
func (c *config) assetName(t target) string {
	return fmt.Sprintf("actionlint_%s_%s.%s", c.version, t.Asset, t.Format)
}

// checksums returns the release's published digests, keyed by asset name.
func (c *config) checksums() (map[string]string, error) {
	name := fmt.Sprintf("actionlint_%s_checksums.txt", c.version)
	data, err := c.readAsset(name)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	sums := map[string]string{}
	for line := range strings.Lines(string(data)) {
		// Each line is "<digest>  <filename>", the sha256sum format.
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sums[strings.TrimPrefix(fields[1], "*")] = fields[0]
	}
	if len(sums) == 0 {
		return nil, fmt.Errorf("%s listed no checksums", name)
	}
	return sums, nil
}

// readAsset returns a release asset, preferring a local copy in the downloads
// directory so a workflow that already fetched the release does not refetch it.
func (c *config) readAsset(name string) ([]byte, error) {
	if c.downloads != "" {
		data, err := os.ReadFile(filepath.Join(c.downloads, name))
		if err == nil {
			return data, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if c.repository == "" {
		return nil, fmt.Errorf("%s is not in %s and no repository was given to download it from", name, c.downloads)
	}
	url := fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", c.repository, c.version, name)
	return get(url, c.token)
}

// verifiedArchive returns a target's release archive, checked against the
// release's own digest before any caller unpacks it. A truncated download or a
// swapped asset fails here.
func (c *config) verifiedArchive(t target, sums map[string]string) ([]byte, error) {
	asset := c.assetName(t)
	archive, err := c.readAsset(asset)
	if err != nil {
		return nil, err
	}
	want, ok := sums[asset]
	if !ok {
		return nil, fmt.Errorf("the checksums manifest lists no digest for %s", asset)
	}
	if got := hex.EncodeToString(sha256Sum(archive)); got != want {
		return nil, fmt.Errorf("%s digest mismatch: manifest says %s, downloaded %s", asset, want, got)
	}
	return archive, nil
}

// manualName is the archive member holding the roff manual. GoReleaser packs it
// at man/<binary>.1 in every archive, so only the base name is needed.
func manualName(tf *targetsFile) string { return tf.Binary + ".1" }

func (c *config) buildPlatformPackage(tf *targetsFile, t target, sums map[string]string) error {
	archive, err := c.verifiedArchive(t, sums)
	if err != nil {
		return err
	}

	// Taken while an archive is open anyway, and required: a release that
	// stopped shipping a manual fails the build here.
	if c.manual == nil {
		manual, err := extractMember(archive, t.Format, manualName(tf))
		if err != nil {
			return err
		}
		c.manual = manual
	}

	exe := tf.Binary
	if t.OS == "win32" {
		exe += ".exe"
	}
	binary, err := extractMember(archive, t.Format, exe)
	if err != nil {
		return err
	}

	name := packageName(tf.PlatformScope, tf.Binary, t.Pkg)
	dir := filepath.Join(c.outDir, t.Pkg)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", exe), binary, 0o755); err != nil {
		return err
	}

	manifest, err := c.platformManifest(tf, t, name)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), manifest, 0o644); err != nil {
		return err
	}
	if err := c.copyLegal(dir); err != nil {
		return err
	}
	readme := fmt.Sprintf(`# %s

The `+"`%s-%s`"+` binary for [`+"`%s`"+`](https://www.npmjs.com/package/%s).

This package is installed automatically as an optional dependency of
`+"`%s`"+`; there is no reason to depend on it directly.
`, name, t.OS, t.CPU, tf.Facade, tf.Facade, tf.Facade)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
		return err
	}

	fmt.Printf("built %s@%s (%s, %d bytes)\n", name, c.version, exe, len(binary))
	return nil
}

// platformManifest renders the package.json of a single platform package. The
// os and cpu fields are what make npm skip this package on every other host.
func (c *config) platformManifest(tf *targetsFile, t target, name string) ([]byte, error) {
	facade, err := c.facadeTemplate()
	if err != nil {
		return nil, err
	}

	o := &object{values: map[string]json.RawMessage{}}
	if err := o.set("name", name); err != nil {
		return nil, err
	}
	if err := o.set("version", c.version); err != nil {
		return nil, err
	}
	if err := o.set("description", fmt.Sprintf("The %s-%s binary for %s.", t.OS, t.CPU, tf.Facade)); err != nil {
		return nil, err
	}
	// Carried over verbatim so every published package points at the same
	// project metadata as the facade.
	for _, key := range []string{"homepage", "bugs", "repository", "funding", "license", "author"} {
		if facade.has(key) {
			o.keys = append(o.keys, key)
			o.values[key] = facade.values[key]
		}
	}
	if err := o.set("os", []string{t.OS}); err != nil {
		return nil, err
	}
	if err := o.set("cpu", []string{t.CPU}); err != nil {
		return nil, err
	}
	if err := o.set("files", []string{"bin/"}); err != nil {
		return nil, err
	}
	// Tells Yarn's PnP linker to keep this package unzipped on disk, since an
	// executable has to be a real file to be spawned.
	if err := o.set("preferUnplugged", true); err != nil {
		return nil, err
	}
	return o.encode()
}

func (c *config) facadeTemplate() (*object, error) {
	data, err := os.ReadFile(filepath.Join(c.npmDir, "facade", "package.json"))
	if err != nil {
		return nil, err
	}
	return parseObject(data)
}

// buildFacade writes the user-facing package: the launcher sources, plus an
// optionalDependencies entry pinned to this exact version for every platform.
func (c *config) buildFacade(tf *targetsFile) error {
	manifest, err := c.facadeTemplate()
	if err != nil {
		return err
	}
	if err := manifest.set("version", c.version); err != nil {
		return err
	}

	// Exact pins: the facade and its binaries are one release. A range lets
	// npm pair a facade with a mismatched binary.
	optional := &object{values: map[string]json.RawMessage{}}
	for _, t := range tf.Targets {
		if err := optional.set(packageName(tf.PlatformScope, tf.Binary, t.Pkg), c.version); err != nil {
			return err
		}
	}
	raw, err := optional.encode()
	if err != nil {
		return err
	}
	manifest.values["optionalDependencies"] = json.RawMessage(bytes.TrimSpace(raw))
	if !manifest.has("optionalDependencies") {
		manifest.keys = append(manifest.keys, "optionalDependencies")
	}

	dir := filepath.Join(c.outDir, "facade")
	if err := os.MkdirAll(filepath.Join(dir, "man"), 0o755); err != nil {
		return err
	}
	src := filepath.Join(c.npmDir, "facade")
	for _, sub := range []string{"bin", "lib"} {
		if err := copyTree(filepath.Join(src, sub), filepath.Join(dir, sub)); err != nil {
			return err
		}
	}
	if err := copyFile(filepath.Join(src, "README.md"), filepath.Join(dir, "README.md")); err != nil {
		return err
	}
	if err := c.copyLegal(dir); err != nil {
		return err
	}

	// The manual is platform-independent and rides on the facade, the one
	// package a user installs.
	manual, err := c.manualFor(tf)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "man", manualName(tf)), manual, 0o644); err != nil {
		return err
	}

	encoded, err := manifest.encode()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), encoded, 0o644); err != nil {
		return err
	}

	fmt.Printf("built %s@%s with %d platform packages\n", tf.Facade, c.version, len(tf.Targets))
	return nil
}

// manualFor returns the roff manual. A full build already unpacked an archive
// and kept it; a facade-only build has to fetch one, and any target will do
// because every archive carries the same manual.
func (c *config) manualFor(tf *targetsFile) ([]byte, error) {
	if c.manual != nil {
		return c.manual, nil
	}
	if len(tf.Targets) == 0 {
		return nil, errors.New("targets.json lists no target to take the manual from")
	}
	t := tf.Targets[0]
	sums, err := c.checksums()
	if err != nil {
		return nil, err
	}
	archive, err := c.verifiedArchive(t, sums)
	if err != nil {
		return nil, err
	}
	manual, err := extractMember(archive, t.Format, manualName(tf))
	if err != nil {
		return nil, err
	}
	c.manual = manual
	return manual, nil
}

// copyLegal places the repository's licence beside a package, npm having no way
// to inherit one. The root is passed in: deriving it from npmDir held only
// while npm/ sat at the top level, and broke silently once the sources moved
// under distribution/.
func (c *config) copyLegal(dir string) error {
	return copyFile(filepath.Join(c.repoRoot, "LICENSE.txt"), filepath.Join(dir, "LICENSE.txt"))
}

// extractMember pulls a single named member out of a release archive. Members
// sit at the archive root or one directory down, so only the base name is
// compared.
func extractMember(archive []byte, format, name string) ([]byte, error) {
	switch format {
	case "tar.gz":
		return extractTarGz(archive, name)
	case "zip":
		return extractZip(archive, name)
	}
	return nil, fmt.Errorf("unsupported archive format %q", format)
}

func extractTarGz(archive []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no %s in the archive", name)
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != name {
			continue
		}
		return readAll(tr, name)
	}
}

func extractZip(archive []byte, name string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || path.Base(f.Name) != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return readAll(rc, name)
	}
	return nil, fmt.Errorf("no %s in the archive", name)
}

// readAll reads a single archive member under the size cap.
func readAll(r io.Reader, name string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxArchive))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%s is empty", name)
	}
	if len(data) == maxArchive {
		return nil, fmt.Errorf("%s exceeds the %d byte limit", name, maxArchive)
	}
	return data, nil
}

func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// copyTree copies a directory's regular files, preserving the executable bit so
// a bin shim stays runnable.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, data, mode)
	})
}

// get retrieves url, retrying briefly for a release asset still propagating to
// the CDN. The token is sent only for github.com; Go's client drops the
// Authorization header across a redirect to another host, as the asset CDN
// requires.
func get(url, token string) ([]byte, error) {
	var lastErr error
	for attempt := range 4 {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<attempt) * time.Second)
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxArchive))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("GET %s: %s", url, resp.Status)
			continue
		}
		if readErr != nil {
			lastErr = readErr
			continue
		}
		return body, nil
	}
	return nil, lastErr
}
