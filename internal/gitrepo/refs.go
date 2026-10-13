package gitrepo

import (
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const symrefMaximumDepth = 5

var errRefBroken = errors.New("broken ref")

type refValue struct {
	target string
	symref bool
	found  bool
}

func resolveBranch(read func(name string) (refValue, error)) (string, error) {
	name := "HEAD"
	symref := false
	for range symrefMaximumDepth {
		value, err := read(name)
		if errors.Is(err, errRefBroken) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if !value.found || !value.symref {
			if !symref {
				return "", nil
			}
			branch, _ := strings.CutPrefix(name, "refs/heads/")
			if branch == name {
				return "", nil
			}
			return branch, nil
		}
		symref = true
		name = value.target
		if !validRefname(name, true) {
			return "", nil
		}
	}
	return "", nil
}

func (r *Repository) refPath(name string) string {
	if rest, ok := strings.CutPrefix(name, "worktrees/"); ok {
		worktree, bare, hasSlash := strings.Cut(rest, "/")
		if !hasSlash || currentWorktreeRef(bare) {
			return filepath.Join(r.CommonDir, "worktrees", worktree, filepath.FromSlash(bare))
		}
	}
	if rest, ok := strings.CutPrefix(name, "main-worktree/"); ok && currentWorktreeRef(rest) {
		return filepath.Join(r.CommonDir, filepath.FromSlash(rest))
	}
	if currentWorktreeRef(name) {
		return filepath.Join(r.GitDir, filepath.FromSlash(name))
	}
	return filepath.Join(r.CommonDir, filepath.FromSlash(name))
}

func currentWorktreeRef(name string) bool {
	if strings.HasPrefix(name, "refs/worktree/") || strings.HasPrefix(name, "refs/bisect/") || strings.HasPrefix(name, "refs/rewritten/") {
		return true
	}
	for _, c := range []byte(name) {
		if (c < 'A' || c > 'Z') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func (r *Repository) readLooseRef(name string, read func(string)) (refValue, error) {
	path := r.refPath(name)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return refValue{}, nil
	}
	if err != nil {
		return refValue{}, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return refValue{}, err
		}
		read(path)
		if target = filepath.ToSlash(target); strings.HasPrefix(target, "refs/") && validRefname(target, false) {
			return refValue{target: target, symref: true, found: true}, nil
		}
		if info, err = os.Stat(path); errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return refValue{}, nil
		} else if err != nil {
			return refValue{}, err
		}
	}
	if info.IsDir() {
		return refValue{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return refValue{}, err
	}
	read(path)
	content := strings.TrimRight(string(data), refSpace)
	if target, ok := strings.CutPrefix(content, "ref:"); ok {
		return refValue{target: strings.TrimLeft(target, refSpace), symref: true, found: true}, nil
	}
	size := 2 * r.hashLength
	if len(content) < size {
		return refValue{}, errRefBroken
	}
	if _, err := hex.DecodeString(content[:size]); err != nil {
		return refValue{}, errRefBroken
	}
	if len(content) > size && !strings.ContainsRune(refSpace, rune(content[size])) {
		return refValue{}, errRefBroken
	}
	return refValue{found: true}, nil
}

const refSpace = " \t\n\v\f\r"

func validRefname(name string, allowOneLevel bool) bool {
	if name == "@" || name == "" {
		return false
	}
	components := 0
	for component := range strings.SplitSeq(name, "/") {
		if component == "" || component[0] == '.' || strings.HasSuffix(component, ".lock") || strings.Contains(component, "..") || strings.Contains(component, "@{") {
			return false
		}
		for _, c := range []byte(component) {
			if c < 0x20 || c == 0x7f || strings.IndexByte(" ~^:?*[\\", c) >= 0 {
				return false
			}
		}
		components++
	}
	if strings.HasSuffix(name, ".") {
		return false
	}
	return allowOneLevel || components >= 2
}
