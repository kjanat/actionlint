package gitrepo

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type objectType uint8

const (
	objectCommit objectType = 1
	objectTree   objectType = 2
	objectBlob   objectType = 3
	objectTag    objectType = 4

	alternateMaximumDepth = 5
)

var errObjectMissing = errors.New("object not found")

type objectSource struct {
	dir       string
	midx      *multiPackIndex
	midxPacks map[string]*packFile
	packs     []*packFile
}

type objectStore struct {
	hashLength int
	sources    []*objectSource
}

func (r *Repository) openObjectStore(settings config) (*objectStore, error) {
	useMidx := true
	if _, ok := settings["core.multipackindex"]; ok {
		var err error
		if useMidx, err = settings.boolean("core.multipackindex"); err != nil {
			return nil, err
		}
	}
	store := &objectStore{hashLength: r.hashLength}
	primary := filepath.Join(r.CommonDir, "objects")
	dirs := []string{primary}
	seen := map[string]bool{r.objectDirKey(realPath(primary)): true}
	var visit func(dir string, depth int)
	visit = func(dir string, depth int) {
		alternates := readAlternates(dir)
		if depth < 0 {
			alternates = append(environmentAlternates(r.base), alternates...)
		}
		if len(alternates) > 0 && depth+1 > alternateMaximumDepth {
			return
		}
		for _, alternate := range alternates {
			key := r.objectDirKey(alternate)
			if info, err := os.Stat(alternate); err != nil || !info.IsDir() || seen[key] {
				continue
			}
			seen[key] = true
			dirs = append(dirs, alternate)
			visit(alternate, depth+1)
		}
	}
	visit(primary, -1)
	for _, dir := range dirs {
		source, err := openObjectSource(dir, r.hashLength, useMidx)
		if err != nil {
			store.close()
			return nil, err
		}
		store.sources = append(store.sources, source)
	}
	return store, nil
}

func (r *Repository) objectDirKey(dir string) string {
	if r.ignoreCase {
		return strings.ToLower(dir)
	}
	return dir
}

func realPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func readAlternates(objects string) []string {
	data, err := os.ReadFile(filepath.Join(objects, "info", "alternates"))
	if err != nil {
		return nil
	}
	return parseAlternates(string(data), '\n', realPath(objects))
}

func environmentAlternates(base string) []string {
	return parseAlternates(os.Getenv("GIT_ALTERNATE_OBJECT_DIRECTORIES"), os.PathListSeparator, base)
}

func parseAlternates(data string, separator byte, base string) []string {
	var out []string
	for rest := data; rest != ""; {
		var entry string
		if unquoted, end, ok := unquoteC(rest); ok {
			entry, rest = unquoted, rest[end:]
			if rest != "" {
				rest = rest[1:]
			}
		} else {
			entry, rest, _ = strings.Cut(rest, string(separator))
			if strings.HasPrefix(entry, "#") {
				continue
			}
		}
		if entry == "" {
			continue
		}
		if !filepath.IsAbs(entry) {
			entry = base + "/" + entry
		}
		if resolved, err := filepath.EvalSymlinks(entry); err == nil {
			out = append(out, resolved)
		}
	}
	return out
}

func unquoteC(s string) (string, int, bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", 0, false
	}
	var out strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			return out.String(), i + 1, true
		case '\\':
			i++
			if i >= len(s) {
				return "", 0, false
			}
			if escaped := strings.IndexByte(`abfnrtv\"`, s[i]); escaped >= 0 {
				out.WriteByte("\a\b\f\n\r\t\v\\\""[escaped])
				continue
			}
			if i+2 < len(s) && s[i] >= '0' && s[i] <= '3' && isOctal(s[i+1]) && isOctal(s[i+2]) {
				out.WriteByte((s[i]-'0')<<6 | (s[i+1]-'0')<<3 | (s[i+2] - '0'))
				i += 2
				continue
			}
			return "", 0, false
		default:
			out.WriteByte(c)
		}
	}
	return "", 0, false
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

func openObjectSource(dir string, hashLength int, useMidx bool) (*objectSource, error) {
	source := &objectSource{dir: dir, midxPacks: make(map[string]*packFile)}
	packDir := filepath.Join(dir, "pack")
	if useMidx {
		midx, err := loadMultiPackIndex(packDir, hashLength)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", packDir, err)
		}
		source.midx = midx
	}
	entries, _ := os.ReadDir(packDir)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".idx") || source.midx != nil && source.midx.contains(name) {
			continue
		}
		pack := newPackFile(filepath.Join(packDir, name))
		if _, err := os.Stat(pack.path); err == nil {
			source.packs = append(source.packs, pack)
		}
	}
	return source, nil
}

func (s *objectStore) close() {
	for _, source := range s.sources {
		for _, pack := range source.packs {
			pack.close()
		}
		for _, pack := range source.midxPacks {
			pack.close()
		}
	}
}

func (s *objectStore) read(oid []byte) (objectType, []byte, error) {
	for _, source := range s.sources {
		kind, data, err := s.readPacked(source, oid)
		if !errors.Is(err, errObjectMissing) {
			return kind, data, err
		}
	}
	for _, source := range s.sources {
		kind, data, err := s.readLoose(source.dir, oid)
		if !errors.Is(err, errObjectMissing) {
			return kind, data, err
		}
	}
	return 0, nil, fmt.Errorf("%s: %w", hex.EncodeToString(oid), errObjectMissing)
}

func (s *objectStore) readPacked(source *objectSource, oid []byte) (objectType, []byte, error) {
	if source.midx != nil {
		name, offset, ok, err := source.midx.find(oid)
		if err != nil {
			return 0, nil, err
		}
		if ok {
			pack := source.midxPacks[name]
			if pack == nil {
				pack = newPackFile(filepath.Join(source.dir, "pack", name))
				source.midxPacks[name] = pack
			}
			if pack.open(s.hashLength) == nil {
				return pack.read(offset)
			}
		}
	}
	for _, pack := range source.packs {
		if pack.open(s.hashLength) != nil {
			continue
		}
		if position, ok := pack.index.find(oid); ok {
			offset, err := pack.index.offset(position)
			if err != nil {
				return 0, nil, err
			}
			return pack.read(offset)
		}
	}
	return 0, nil, errObjectMissing
}

func (s *objectStore) readLoose(dir string, oid []byte) (objectType, []byte, error) {
	name := hex.EncodeToString(oid)
	path := filepath.Join(dir, name[:2], name[2:])
	file, err := os.Open(path)
	if err != nil {
		return 0, nil, errObjectMissing
	}
	defer file.Close()
	corrupt := func(reason string) error {
		return fmt.Errorf("loose object %s (stored in %s) is corrupt: %s", name, path, reason)
	}
	reader, err := zlib.NewReader(file)
	if err != nil {
		return 0, nil, corrupt(err.Error())
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return 0, nil, corrupt(err.Error())
	}
	header, content, ok := bytes.Cut(data, []byte{0})
	if !ok {
		return 0, nil, corrupt("the header is unterminated")
	}
	typeName, sizeText, ok := strings.Cut(string(header), " ")
	if !ok {
		return 0, nil, corrupt("the header has no size")
	}
	kind, ok := map[string]objectType{"commit": objectCommit, "tree": objectTree, "blob": objectBlob, "tag": objectTag}[typeName]
	if !ok {
		return 0, nil, corrupt("unknown object type " + strconv.Quote(typeName))
	}
	size, err := strconv.ParseUint(sizeText, 10, 64)
	if err != nil {
		return 0, nil, corrupt("bad object size")
	}
	if size != uint64(len(content)) {
		return 0, nil, corrupt("the size does not match the content")
	}
	return kind, content, nil
}

type treeEntry struct {
	mode uint32
	name []byte
	oid  []byte
}

func parseTree(data []byte, hashLength int) ([]treeEntry, error) {
	var entries []treeEntry
	for len(data) > 0 {
		if len(data) < hashLength+3 || data[len(data)-(hashLength+1)] != 0 {
			return nil, errors.New("too-short tree object")
		}
		space := bytes.IndexByte(data, ' ')
		if space <= 0 {
			return nil, errors.New("malformed mode in tree entry")
		}
		var mode uint32
		for _, c := range data[:space] {
			if c < '0' || c > '7' {
				return nil, errors.New("malformed mode in tree entry")
			}
			mode = mode<<3 + uint32(c-'0')
		}
		rest := data[space+1:]
		end := bytes.IndexByte(rest, 0)
		if end == 0 {
			return nil, errors.New("empty filename in tree entry")
		}
		entries = append(entries, treeEntry{mode: canonicalMode(uint16(mode)), name: rest[:end], oid: rest[end+1 : end+1+hashLength]})
		data = rest[end+1+hashLength:]
	}
	return entries, nil
}

func canonicalMode(mode uint16) uint32 {
	switch uint32(mode) & modeTypeMask {
	case modeRegular:
		return modeRegular | permissions(uint32(mode))
	case modeSymlink:
		return modeSymlink
	case modeDirectory:
		return modeDirectory
	}
	return modeGitlink
}

func permissions(mode uint32) uint32 {
	if mode&0o100 != 0 {
		return 0o755
	}
	return 0o644
}

var errTreeUnreadable = errors.New("unreadable tree")

func (s *objectStore) walkTree(ctx context.Context, oid []byte, base []byte, depth, maxDepth int, rules pathRules, add func(indexEntry)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxDepth {
		return fmt.Errorf("%w: exceeded maximum allowed tree depth", errTreeUnreadable)
	}
	kind, data, err := s.read(oid)
	if errors.Is(err, errObjectMissing) {
		return fmt.Errorf("%w: could not read %s", errTreeUnreadable, hex.EncodeToString(oid))
	}
	if err != nil {
		return err
	}
	if kind != objectTree {
		return fmt.Errorf("%w: object %s not a tree", errTreeUnreadable, hex.EncodeToString(oid))
	}
	entries, err := parseTree(data, s.hashLength)
	if err != nil {
		return fmt.Errorf("tree %s: %w", hex.EncodeToString(oid), err)
	}
	for _, entry := range entries {
		path := slices.Concat(base, entry.name)
		if entry.mode == modeDirectory {
			if err := s.walkTree(ctx, entry.oid, append(path, '/'), depth+1, maxDepth, rules, add); err != nil {
				return err
			}
			continue
		}
		if !rules.valid(path, entry.mode) {
			return fmt.Errorf("invalid path '%s'", path)
		}
		add(indexEntry{path: path, mode: entry.mode, skipWorktree: true, oid: entry.oid})
	}
	return nil
}
