package execution

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/fsread"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

type DirectoryKind uint8

const (
	DirectoryUnspecified DirectoryKind = iota
	DirectoryKnown
	DirectoryUnknown
	DirectoryActionKnown
)

type Directory struct {
	Kind DirectoryKind
	Path string
}

type Paths struct {
	Platform          runnerlang.Platform
	Workspace         string
	Analysis          string
	Checkout          string
	CheckoutUnknown   bool
	ActionPath        string
	ActionRunnerPath  string
	ActionIndependent bool
	Placements        CheckoutPlacement
	CaseInsensitive   bool
	Reader            *fsread.Reader
}

func DirectoryValue(semantic value.Value) Directory {
	if semantic.State() == value.Missing {
		return Directory{}
	}
	if text, known := semantic.ScalarString(); known {
		return Directory{DirectoryKnown, text}
	}
	if semantic.State() != value.Dynamic {
		return Directory{Kind: DirectoryUnknown}
	}
	return Directory{Kind: DirectoryUnknown}
}

func DefaultDirectory(defaults value.Value) Directory {
	if defaults.State() == value.Missing {
		return Directory{}
	}
	if !defaults.Known() {
		return Directory{Kind: DirectoryUnknown}
	}
	if defaults.Kind() != value.Object {
		return Directory{Kind: DirectoryUnknown}
	}
	field, present := defaults.Prop("working-directory")
	if !present {
		return Directory{}
	}
	return DirectoryValue(field)
}

func (paths Paths) EffectiveDirectory(runDirectory value.Value, jobDir, workflowDir Directory) Directory {
	directory := Directory{DirectoryKnown, ""}
	for _, candidate := range []Directory{paths.WorkingDirectory(runDirectory), jobDir, workflowDir} {
		if candidate.Kind != DirectoryUnspecified {
			directory = candidate
			break
		}
	}
	if normalized, known := LocalDirectoryPath(directory.Path, paths.Platform); known {
		directory.Path = normalized
	} else {
		directory.Kind = DirectoryUnknown
	}
	return directory
}

func (paths Paths) WorkingDirectory(semantic value.Value) Directory {
	directory := DirectoryValue(semantic)
	if directory.Kind != DirectoryUnknown {
		return directory
	}
	if semantic.State() != value.Dynamic {
		return directory
	}
	expression, ok := strings.CutPrefix(semantic.Source(), "${{")
	if !ok {
		return directory
	}
	name, suffix, closed := strings.Cut(expression, "}}")
	suffix, representable := RunnerDirectoryPath(suffix, paths.Platform)
	if !closed || !representable || strings.Contains(suffix, "${{") || suffix != "" && !strings.HasPrefix(suffix, "/") {
		return directory
	}
	base := "."
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "github.workspace":
	case "github.action_path":
		if paths.ActionPath == "" || paths.Workspace == "" {
			return directory
		}
		relative, err := filepath.Rel(paths.Workspace, paths.ActionPath)
		if err != nil || !filepath.IsLocal(relative) {
			return directory
		}
		if paths.ActionIndependent {
			return Directory{DirectoryActionKnown, filepath.ToSlash(relative) + suffix}
		}
		base = JoinPath(paths.Checkout, filepath.ToSlash(relative))
		if paths.ActionRunnerPath != "" {
			base = paths.ActionRunnerPath
		}
	default:
		return directory
	}
	// Keep runner paths relative until local() translates the self checkout.
	return Directory{DirectoryKnown, base + suffix}
}

func (paths Paths) Resolve(directory Directory) Directory {
	unknown := Directory{DirectoryUnknown, paths.Analysis}
	if directory.Kind == DirectoryUnknown {
		return unknown
	}
	paths = paths.DirectoryOrigin(directory)
	local, ok := paths.AnalysisPath(directory.Path)
	if !ok {
		return unknown
	}
	local, err := filepath.EvalSymlinks(local)
	if err != nil {
		return unknown
	}
	info, err := os.Stat(local)
	if err != nil || !info.IsDir() {
		return unknown
	}
	return Directory{DirectoryKnown, local}
}

func (paths Paths) DirectoryOrigin(directory Directory) Paths {
	if directory.Kind == DirectoryActionKnown {
		paths.Checkout, paths.CheckoutUnknown, paths.Placements = "", false, CheckoutPlacement{}
	}
	return paths
}

func (paths Paths) CheckoutFor(relativePath string) (string, bool) {
	if paths.Placements.Present() {
		placement := paths.Placements.Matching(relativePath)
		if !placement.Present() || placement.Details().Directory.Kind != DirectoryKnown {
			return "", false
		}
		if placement.Details().CaseInsensitive && placement.Details().Directory.Path != "" && placement.Details().Directory.Path != "." {
			prefix := path.Clean(placement.Details().Directory.Path)
			if prefix == "." {
				return prefix, true
			}
			// Keep the caller's prefix spelling for host-side relative-path mapping.
			return path.Clean(relativePath)[:len(prefix)], true
		}
		return placement.Details().Directory.Path, true
	}
	return paths.Checkout, !paths.CheckoutUnknown
}

// Local translates a workspace-relative runner path to the local self checkout.
func (paths Paths) Local(relativePath string) (string, bool) {
	relativePath, representable := RunnerRelativePath(relativePath, paths.Platform)
	if !representable {
		return "", false
	}
	path, known := paths.AnalysisPath(relativePath)
	if !known {
		return "", false
	}
	relative, err := filepath.Rel(paths.Workspace, path)
	return path, err == nil && filepath.IsLocal(relative)
}

// AnalysisPath resolves available files, including those outside the indexed repository.
func (paths Paths) AnalysisPath(value string) (string, bool) {
	relativePath, known := LocalDirectoryPath(value, paths.Platform)
	if !known {
		return "", false
	}
	if filepath.IsAbs(relativePath) {
		return relativePath, true
	}
	if strings.HasPrefix(relativePath, "/") || strings.ContainsRune(relativePath, '\x00') {
		return "", false
	}
	checkout, known := paths.CheckoutFor(relativePath)
	if !known {
		return "", false
	}
	caseInsensitive := paths.CaseInsensitive
	if placement := paths.Placements.Matching(relativePath); placement.Present() {
		caseInsensitive = placement.Details().CaseInsensitive
	}
	relativePath = filepath.FromSlash(relativePath)
	if checkout != "" {
		var err error
		relativePath, err = filepath.Rel(filepath.FromSlash(checkout), filepath.Clean(relativePath))
		if err != nil {
			return "", false
		}
	}
	if caseInsensitive && filepath.IsLocal(relativePath) {
		canonical, known := caseInsensitiveRepositoryPath(paths.Workspace, filepath.ToSlash(relativePath), paths.Reader)
		if !known {
			return "", false
		}
		relativePath = filepath.FromSlash(canonical)
	}
	return filepath.Join(paths.Workspace, relativePath), true
}

func RunnerRelativePath(value string, platform runnerlang.Platform) (string, bool) {
	value, known := RunnerDirectoryPath(value, platform)
	// Resolve only paths relative to the checkout on the runner.
	return value, known && !filepath.IsAbs(value) && !strings.HasPrefix(value, "/") && !strings.ContainsRune(value, '\x00')
}

// JoinPath preserves path components until indexed symlink traversal is checked.
func JoinPath(base, relative string) string {
	if base == "" {
		return relative
	}
	// Preserve components until symlink traversal has been checked in the index.
	return base + "/" + relative
}
