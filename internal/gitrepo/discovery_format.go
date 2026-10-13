package gitrepo

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

type discoveryFormat struct {
	version        int64
	worktreeConfig bool
	sha256         bool
	bare           int
	workTree       string
	hasWorkTree    bool
	unknown        []string
	v1Only         []string
}

func newDiscoveryFormat() discoveryFormat {
	return discoveryFormat{version: -1, bare: -1}
}

func readDiscoveryFormat(gitDir, commonDir string, shared bool) (discoveryFormat, error) {
	format := newDiscoveryFormat()
	path := filepath.Join(commonDir, "config")
	data, err := readOptional(path)
	if err == nil {
		err = parseConfig(path, data, format.visit)
	}
	if err != nil {
		return format, fmt.Errorf("%s: %w", path, err)
	}
	if format.version == -1 {
		format = newDiscoveryFormat()
	}
	if format.version < 0 {
		return format, nil
	}
	if err := format.verify(); err != nil {
		return format, err
	}
	if format.worktreeConfig {
		path := filepath.Join(gitDir, "config.worktree")
		data, err := readOptional(path)
		if err == nil {
			err = parseConfig(path, data, format.visitWorktree)
		}
		if err != nil {
			return format, fmt.Errorf("%s: %w", path, err)
		}
		shared = false
	}
	if shared {
		format.bare = -1
		format.workTree, format.hasWorkTree = "", false
	}
	return format, nil
}

func (f *discoveryFormat) visit(section, name string, value configValue) error {
	key := section + "." + name
	if key == "core.repositoryformatversion" {
		if value.bare {
			return fmt.Errorf("missing value for '%s'", key)
		}
		version, ok := parseInt(value.text)
		if !ok {
			return fmt.Errorf("bad numeric config value %q for '%s'", value.text, key)
		}
		f.version = version
		return nil
	}
	if extension, ok := strings.CutPrefix(key, "extensions."); ok {
		if err := f.extension(extension, value); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		return nil
	}
	return f.visitWorktree(section, name, value)
}

func (f *discoveryFormat) visitWorktree(section, name string, value configValue) error {
	switch key := section + "." + name; key {
	case "core.bare":
		bare, err := value.boolean()
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		f.bare = 0
		if bare {
			f.bare = 1
		}
	case "core.worktree":
		if value.bare {
			return fmt.Errorf("missing value for '%s'", key)
		}
		f.workTree, f.hasWorkTree = value.text, true
	}
	return nil
}

func (f *discoveryFormat) extension(name string, value configValue) error {
	var err error
	switch name {
	case "noop":
		return nil
	case "partialclone":
		return value.nonbool()
	case "preciousobjects":
		_, err = value.boolean()
		return err
	case "worktreeconfig":
		f.worktreeConfig, err = value.boolean()
		return err
	case "noop-v1":
	case "objectformat", "compatobjectformat":
		if err := value.nonbool(); err != nil {
			return err
		}
		if value.text != "sha1" && value.text != "sha256" {
			return fmt.Errorf("invalid value %q", value.text)
		}
		if name == "compatobjectformat" && slices.Contains(f.v1Only, name) {
			return errors.New("already specified")
		}
		if name == "objectformat" {
			f.sha256 = value.text == "sha256"
		}
	case "refstorage":
		if err := value.nonbool(); err != nil {
			return err
		}
		if backend, _, _ := strings.Cut(value.text, "://"); backend != "files" && backend != "reftable" {
			return fmt.Errorf("invalid value %q", value.text)
		}
	case "relativeworktrees", "submodulepathconfig":
		if _, err = value.boolean(); err != nil {
			return err
		}
	default:
		f.unknown = append(f.unknown, name)
		return nil
	}
	f.v1Only = append(f.v1Only, name)
	return nil
}

func (v configValue) nonbool() error {
	if v.bare {
		return errors.New("missing value")
	}
	return nil
}

func (f *discoveryFormat) verify() error {
	switch {
	case f.version > 1:
		return fmt.Errorf("expected git repo version <= 1, found %d", f.version)
	case f.version == 1 && len(f.unknown) > 0:
		return fmt.Errorf("unknown repository extensions found: %s", strings.Join(f.unknown, ", "))
	case f.version == 0 && len(f.v1Only) > 0:
		return fmt.Errorf("repo version is 0, but v1-only extensions found: %s", strings.Join(f.v1Only, ", "))
	}
	return nil
}

func (f *discoveryFormat) resolveWorkTree(found *location) (string, error) {
	switch {
	case f.bare > 0:
		return "", nil
	case f.hasWorkTree:
		path := filepath.FromSlash(f.workTree)
		if path == "" {
			return "", errors.New("core.worktree is empty")
		}
		if !isAbsolutePath(path) {
			path = joinRaw(found.gitDir, path)
		}
		return filepath.EvalSymlinks(path)
	case found.bare:
		return "", nil
	}
	return found.dir, nil
}

func hexValue(b byte) int {
	switch {
	case isDigit(b):
		return int(b - '0')
	case 'a' <= b && b <= 'f':
		return int(b-'a') + 10
	case 'A' <= b && b <= 'F':
		return int(b-'A') + 10
	}
	return 16
}
