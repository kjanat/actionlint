package gitrepo

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"unicode/utf8"
)

const (
	indexHeaderSize       = 12
	indexStatBeforeMode   = 24
	indexStatAfterMode    = 12
	indexExtendedFlag     = 0x4000
	indexStageShift       = 12
	indexNameLengthMask   = 0xfff
	indexIntentToAddFlag  = 0x2000
	indexSkipWorktreeFlag = 0x4000
	indexKnownExtended    = indexIntentToAddFlag | indexSkipWorktreeFlag

	modeTypeMask  = 0o170000
	modeRegular   = 0o100000
	modeSymlink   = 0o120000
	modeDirectory = 0o040000
	modeGitlink   = 0o160000

	defaultMaxTreeDepth = 2048
)

type IndexEntry struct {
	Path         string
	Mode         uint32
	Stage        uint8
	IntentToAdd  bool
	SkipWorktree bool
	ObjectID     string
}

func (e IndexEntry) IsFile() bool {
	kind := e.Mode & modeTypeMask
	return kind == modeRegular || kind == modeSymlink
}

func (e IndexEntry) IsSparseDirectory() bool {
	return e.Mode == modeDirectory
}

type indexEntry struct {
	path         []byte
	mode         uint32
	stage        uint8
	intentToAdd  bool
	skipWorktree bool
	oid          []byte
}

func (e indexEntry) public() IndexEntry {
	return IndexEntry{
		Path:         string(e.path),
		Mode:         e.mode,
		Stage:        e.stage,
		IntentToAdd:  e.intentToAdd,
		SkipWorktree: e.skipWorktree,
		ObjectID:     hex.EncodeToString(e.oid),
	}
}

type indexFile struct {
	entries  []indexEntry
	link     *indexLink
	sparse   bool
	checksum []byte
}

type indexLink struct {
	base     []byte
	deleted  ewahBitmap
	replaced ewahBitmap
}

type indexSource struct {
	hashLength int
	loadShared func(name string) ([]byte, error)
	pathRules  func() (pathRules, error)
}

func (r *Repository) IndexPath() string {
	return filepath.Join(r.GitDir, "index")
}

func (r *Repository) Index() ([]IndexEntry, error) {
	index, err := r.readIndex()
	if err != nil || index == nil {
		return nil, err
	}
	return publicEntries(index.entries), nil
}

func (r *Repository) ExpandedIndex(ctx context.Context) ([]IndexEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	index, err := r.readIndex()
	if err != nil || index == nil {
		return nil, err
	}
	if !index.sparse {
		return publicEntries(index.entries), nil
	}
	settings, err := r.config()
	if err != nil {
		return nil, err
	}
	maxDepth := defaultMaxTreeDepth
	for _, value := range settings["core.maxtreedepth"] {
		if value.bare {
			return nil, errors.New("missing value for 'core.maxtreedepth'")
		}
		number, ok := parseInt(value.text)
		if !ok {
			return nil, fmt.Errorf("bad numeric config value %q for 'core.maxtreedepth'", value.text)
		}
		maxDepth = int(number)
	}
	rules, err := pathRulesFrom(settings)
	if err != nil {
		return nil, err
	}
	store, err := r.openObjectStore(settings)
	if err != nil {
		return nil, err
	}
	defer store.close()
	var expanded []indexEntry
	for _, entry := range index.entries {
		if entry.mode != modeDirectory {
			expanded = append(expanded, entry)
			continue
		}
		err := store.walkTree(ctx, entry.oid, entry.path, 0, maxDepth, rules, func(e indexEntry) {
			expanded = append(expanded, e)
		})
		if err != nil && !errors.Is(err, errTreeUnreadable) {
			return nil, fmt.Errorf("expand %s: %w", entry.path, err)
		}
	}
	return publicEntries(expanded), nil
}

func (r *Repository) TrackedFiles(ctx context.Context) ([]string, error) {
	entries, err := r.ExpandedIndex(ctx)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsFile() {
			paths = append(paths, entry.Path)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}

func (r *Repository) readIndex() (*indexFile, error) {
	path := r.IndexPath()
	data, err := readOptional(path)
	if err != nil || data == nil {
		return nil, err
	}
	r.reads.record(path)
	index, err := readIndex(data, indexSource{
		hashLength: r.hashLength,
		loadShared: func(name string) ([]byte, error) {
			shared := filepath.Join(r.GitDir, "sharedindex."+name)
			data, err := readOptional(shared)
			if err == nil && data == nil {
				err = fmt.Errorf("the shared index sharedindex.%s is missing", name)
			}
			if err == nil {
				r.reads.record(shared)
			}
			return data, err
		},
		pathRules: func() (pathRules, error) {
			settings, err := r.config()
			if err != nil {
				return pathRules{}, err
			}
			return pathRulesFrom(settings)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", path, err)
	}
	return index, nil
}

func publicEntries(entries []indexEntry) []IndexEntry {
	result := make([]IndexEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.public())
	}
	return result
}

func readIndex(data []byte, source indexSource) (*indexFile, error) {
	index, err := parseIndex(data, source.hashLength)
	if err != nil {
		return nil, err
	}
	if index.link == nil || !slices.ContainsFunc(index.link.base, func(b byte) bool { return b != 0 }) {
		return index, nil
	}
	name := hex.EncodeToString(index.link.base)
	shared, err := source.loadShared(name)
	if err != nil {
		return nil, err
	}
	base, err := parseIndex(shared, source.hashLength)
	if err != nil {
		return nil, fmt.Errorf("shared index %s: %w", name, err)
	}
	if !bytes.Equal(base.checksum, index.link.base) {
		return nil, fmt.Errorf("broken index, expect %s in sharedindex.%s, got %s", name, name, hex.EncodeToString(base.checksum))
	}
	if index.entries, err = mergeSplitIndex(base.entries, index.entries, index.link, source.pathRules); err != nil {
		return nil, err
	}
	return index, nil
}

func mergeSplitIndex(base, split []indexEntry, link *indexLink, loadRules func() (pathRules, error)) ([]indexEntry, error) {
	merged := slices.Clone(base)
	replaced := 0
	err := link.replaced.each(func(position uint64) error {
		if position >= uint64(len(merged)) {
			return fmt.Errorf("position for replacement %d exceeds base index size %d", position, len(merged))
		}
		if replaced >= len(split) {
			return fmt.Errorf("too many replacements (%d vs %d)", replaced, len(split))
		}
		replacement := split[replaced]
		if len(replacement.path) != 0 {
			return fmt.Errorf("corrupt link extension, entry %d should have zero length name", position)
		}
		replacement.path = merged[position].path
		merged[position] = replacement
		replaced++
		return nil
	})
	if err != nil {
		return nil, err
	}
	deleted := make(map[uint64]bool)
	err = link.deleted.each(func(position uint64) error {
		if position >= uint64(len(merged)) {
			return fmt.Errorf("position for delete %d exceeds base index size %d", position, len(merged))
		}
		deleted[position] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	entries := make([]indexEntry, 0, len(merged)+len(split)-replaced)
	for position, entry := range merged {
		if !deleted[uint64(position)] {
			entries = append(entries, entry)
		}
	}
	var rules *pathRules
	for i, entry := range split[replaced:] {
		if len(entry.path) == 0 {
			return nil, fmt.Errorf("corrupt link extension, entry %d should have non-zero length name", replaced+i)
		}
		if rules == nil {
			loaded, err := loadRules()
			if err != nil {
				return nil, err
			}
			rules = &loaded
		}
		entries = addIndexEntry(entries, entry, *rules)
	}
	return entries, nil
}

func compareIndexNames(a []byte, aStage uint8, b []byte, bStage uint8) int {
	if c := bytes.Compare(a, b); c != 0 {
		return c
	}
	switch {
	case aStage < bStage:
		return -1
	case aStage > bStage:
		return 1
	}
	return 0
}

func indexNameStagePosition(entries []indexEntry, name []byte, stage uint8) (int, bool) {
	first, last := 0, len(entries)
	for last > first {
		next := first + (last-first)>>1
		switch c := compareIndexNames(name, stage, entries[next].path, entries[next].stage); {
		case c == 0:
			return next, true
		case c < 0:
			last = next
		default:
			first = next + 1
		}
	}
	return first, false
}

func addIndexEntry(entries []indexEntry, entry indexEntry, rules pathRules) []indexEntry {
	var position int
	var found bool
	if len(entries) > 0 && bytes.Compare(entry.path, entries[len(entries)-1].path) > 0 {
		position = len(entries)
	} else {
		position, found = indexNameStagePosition(entries, entry.path, entry.stage)
	}
	if found {
		entries[position] = entry
		return entries
	}
	if entry.stage == 0 {
		for position < len(entries) && bytes.Equal(entries[position].path, entry.path) {
			entries = slices.Delete(entries, position, position+1)
		}
	}
	if !rules.valid(entry.path, entry.mode) {
		return entries
	}
	return slices.Insert(entries, position, entry)
}

func parseIndex(data []byte, hashLength int) (*indexFile, error) {
	if len(data) < indexHeaderSize+hashLength {
		return nil, errors.New("index file smaller than expected")
	}
	body := data[:len(data)-hashLength]
	if string(body[:4]) != "DIRC" {
		return nil, fmt.Errorf("bad signature 0x%08x", binary.BigEndian.Uint32(body))
	}
	version := binary.BigEndian.Uint32(body[4:])
	if version < 2 || version > 4 {
		return nil, fmt.Errorf("bad index version %d", version)
	}
	count := binary.BigEndian.Uint32(body[8:])
	reader := indexReader{data: body, pos: indexHeaderSize}
	index := &indexFile{checksum: data[len(data)-hashLength:]}
	for i := range count {
		var previous *indexEntry
		if i > 0 {
			previous = &index.entries[i-1]
		}
		entry, err := reader.entry(version, hashLength, previous)
		if err != nil {
			return nil, err
		}
		index.sparse = index.sparse || entry.mode == modeDirectory
		index.entries = append(index.entries, entry)
	}
	for len(reader.data)-reader.pos >= 8 {
		signature, _ := reader.bytes(4)
		size, _ := reader.uint32()
		payload, err := reader.bytes(int(size))
		if err != nil {
			return nil, fmt.Errorf("the %s extension is truncated: %w", signature, err)
		}
		switch string(signature) {
		case "link":
			if index.link, err = parseLink(payload, hashLength); err != nil {
				return nil, err
			}
		case "sdir":
			index.sparse = true
		default:
			if signature[0] < 'A' || signature[0] > 'Z' {
				return nil, fmt.Errorf("index uses %s extension, which we do not understand", signature)
			}
		}
	}
	return index, nil
}

func parseLink(payload []byte, hashLength int) (*indexLink, error) {
	if len(payload) < hashLength {
		return nil, errors.New("corrupt link extension (too short)")
	}
	link := &indexLink{base: payload[:hashLength]}
	reader := indexReader{data: payload[hashLength:]}
	if reader.done() {
		return link, nil
	}
	var err error
	if link.deleted, err = reader.ewah(); err != nil {
		return nil, fmt.Errorf("corrupt delete bitmap in link extension: %w", err)
	}
	if link.replaced, err = reader.ewah(); err != nil {
		return nil, fmt.Errorf("corrupt replace bitmap in link extension: %w", err)
	}
	if !reader.done() {
		return nil, errors.New("garbage at the end of link extension")
	}
	return link, nil
}

type indexReader struct {
	data []byte
	pos  int
}

func (r *indexReader) done() bool { return r.pos >= len(r.data) }

func (r *indexReader) bytes(n int) ([]byte, error) {
	if n < 0 || len(r.data)-r.pos < n {
		return nil, fmt.Errorf("the index ends after %d bytes", len(r.data))
	}
	out := r.data[r.pos : r.pos+n]
	r.pos += n
	return out, nil
}

func (r *indexReader) uint32() (uint32, error) {
	b, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

func (r *indexReader) nulTerminated() ([]byte, error) {
	end := bytes.IndexByte(r.data[r.pos:], 0)
	if end < 0 {
		return nil, errors.New("the index has an unterminated name")
	}
	out := r.data[r.pos : r.pos+end]
	r.pos += end + 1
	return out, nil
}

func (r *indexReader) varint() (uint64, error) {
	b, err := r.bytes(1)
	if err != nil {
		return 0, err
	}
	value := uint64(b[0] & 0x7f)
	for b[0]&0x80 != 0 {
		value++
		if value == 0 || value>>(64-7) != 0 {
			return 0, errors.New("the index has an overflowing name prefix length")
		}
		if b, err = r.bytes(1); err != nil {
			return 0, err
		}
		value = value<<7 | uint64(b[0]&0x7f)
	}
	return value, nil
}

func (r *indexReader) entry(version uint32, hashLength int, previous *indexEntry) (indexEntry, error) {
	start := r.pos
	fixed, err := r.bytes(indexStatBeforeMode + 4 + indexStatAfterMode + hashLength + 2)
	if err != nil {
		return indexEntry{}, err
	}
	mode := binary.BigEndian.Uint32(fixed[indexStatBeforeMode:])
	oid := fixed[indexStatBeforeMode+4+indexStatAfterMode : len(fixed)-2]
	flags := binary.BigEndian.Uint16(fixed[len(fixed)-2:])
	var extended uint16
	if flags&indexExtendedFlag != 0 {
		raw, err := r.bytes(2)
		if err != nil {
			return indexEntry{}, err
		}
		if extended = binary.BigEndian.Uint16(raw); extended&^indexKnownExtended != 0 {
			return indexEntry{}, fmt.Errorf("unknown index entry format 0x%08x", uint32(extended)<<16)
		}
	}
	var path []byte
	if version == 4 {
		strip, err := r.varint()
		if err != nil {
			return indexEntry{}, err
		}
		var prefix []byte
		if previous != nil {
			if strip > uint64(len(previous.path)) {
				return indexEntry{}, fmt.Errorf("malformed name field in the index, near path '%s'", previous.path)
			}
			prefix = previous.path[:len(previous.path)-int(strip)]
		}
		suffix, err := r.nulTerminated()
		if err != nil {
			return indexEntry{}, err
		}
		path = slices.Concat(prefix, suffix)
	} else {
		if path, err = r.nulTerminated(); err != nil {
			return indexEntry{}, err
		}
		length := r.pos - start
		if _, err := r.bytes((length+7)/8*8 - length); err != nil {
			return indexEntry{}, err
		}
	}
	if nameLength := int(flags & indexNameLengthMask); nameLength != indexNameLengthMask && len(path) != nameLength {
		return indexEntry{}, fmt.Errorf("the index entry %q does not match its length %d", path, nameLength)
	}
	return indexEntry{
		path:         path,
		mode:         mode,
		stage:        uint8(flags >> indexStageShift & 3),
		intentToAdd:  extended&indexIntentToAddFlag != 0,
		skipWorktree: extended&indexSkipWorktreeFlag != 0,
		oid:          oid,
	}, nil
}

type pathRules struct {
	ntfs bool
	hfs  bool
}

func pathRulesFrom(settings config) (pathRules, error) {
	rules := pathRules{ntfs: true, hfs: runtime.GOOS == "darwin"}
	for key, target := range map[string]*bool{"core.protectntfs": &rules.ntfs, "core.protecthfs": &rules.hfs} {
		if _, ok := settings[key]; !ok {
			continue
		}
		value, err := settings.boolean(key)
		if err != nil {
			return pathRules{}, fmt.Errorf("%s: %w", key, err)
		}
		*target = value
	}
	return rules, nil
}

func byteAt(s []byte, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return 0
}

func (rules pathRules) valid(path []byte, mode uint32) bool {
	symlink := mode&modeTypeMask == modeSymlink
	pos := 0
	next := func() byte {
		c := byteAt(path, pos)
		pos++
		return c
	}
	var c byte
	atSeparator := true
	for {
		if atSeparator {
			rest := path[pos:]
			if rules.hfs && (isHFSDot(rest, "git") || symlink && isHFSDot(rest, "gitmodules")) {
				return false
			}
			if rules.ntfs && (isNTFSDotGit(rest) || symlink && isNTFSDot(rest, "gitmodules", "gi7eba")) {
				return false
			}
			c = next()
			if c == '.' && !verifyDotfile(path[pos:], mode) || c == '/' {
				return false
			}
			if c == 0 {
				return mode&modeTypeMask == modeDirectory
			}
		} else if c == '\\' && rules.ntfs {
			rest := path[pos:]
			if isNTFSDotGit(rest) || symlink && isNTFSDot(rest, "gitmodules", "gi7eba") {
				return false
			}
		}
		c = next()
		if c == 0 {
			return true
		}
		atSeparator = c == '/'
	}
}

func verifyDotfile(rest []byte, mode uint32) bool {
	switch byteAt(rest, 0) {
	case 0, '/':
		return false
	case 'g', 'G':
		if lower(byteAt(rest, 1)) != 'i' || lower(byteAt(rest, 2)) != 't' {
			return true
		}
		if c := byteAt(rest, 3); c == 0 || c == '/' {
			return false
		}
		if mode&modeTypeMask == modeSymlink && hasPrefixFold(rest, 3, "modules") {
			if c := byteAt(rest, 10); c == 0 || c == '/' {
				return false
			}
		}
	case '.':
		if c := byteAt(rest, 1); c == 0 || c == '/' {
			return false
		}
	}
	return true
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func isNTFSDotGit(name []byte) bool {
	var i int
	switch byteAt(name, 0) {
	case '.':
		if lower(byteAt(name, 1)) != 'g' || lower(byteAt(name, 2)) != 'i' || lower(byteAt(name, 3)) != 't' {
			return false
		}
		i = 4
	case 'g', 'G':
		if lower(byteAt(name, 1)) != 'i' || lower(byteAt(name, 2)) != 't' || byteAt(name, 3) != '~' || byteAt(name, 4) != '1' {
			return false
		}
		i = 5
	default:
		return false
	}
	for ; ; i++ {
		switch c := byteAt(name, i); c {
		case 0, '/', '\\', ':':
			return true
		case '.', ' ':
		default:
			return false
		}
	}
}

func onlySpacesAndPeriods(name []byte, i int) bool {
	for ; ; i++ {
		switch byteAt(name, i) {
		case 0, ':':
			return true
		case ' ', '.':
		default:
			return false
		}
	}
}

func hasPrefixFold(name []byte, start int, prefix string) bool {
	for j := range len(prefix) {
		if lower(byteAt(name, start+j)) != prefix[j] {
			return false
		}
	}
	return true
}

func isNTFSDot(name []byte, dotgit, shortPrefix string) bool {
	if byteAt(name, 0) == '.' && hasPrefixFold(name, 1, dotgit) {
		return onlySpacesAndPeriods(name, len(dotgit)+1)
	}
	if hasPrefixFold(name, 0, dotgit[:6]) && byteAt(name, 6) == '~' && byteAt(name, 7) >= '1' && byteAt(name, 7) <= '4' {
		return onlySpacesAndPeriods(name, 8)
	}
	sawTilde := false
	for i := 0; i < 8; i++ {
		c := byteAt(name, i)
		switch {
		case c == 0:
			return false
		case sawTilde:
			if c < '0' || c > '9' {
				return false
			}
		case c == '~':
			i++
			if c := byteAt(name, i); c < '1' || c > '9' {
				return false
			}
			sawTilde = true
		case i >= 6, c&0x80 != 0, lower(c) != shortPrefix[i]:
			return false
		}
	}
	return onlySpacesAndPeriods(name, 8)
}

func nextHFSRune(rest *[]byte) rune {
	for {
		if len(*rest) == 0 {
			return 0
		}
		r, size := utf8.DecodeRune(*rest)
		if r == utf8.RuneError && size <= 1 {
			*rest = nil
			return 0
		}
		*rest = (*rest)[size:]
		switch r {
		case 0x200c, 0x200d, 0x200e, 0x200f, 0x202a, 0x202b, 0x202c, 0x202d, 0x202e,
			0x206a, 0x206b, 0x206c, 0x206d, 0x206e, 0x206f, 0xfeff:
			continue
		}
		return r
	}
}

func isHFSDot(path []byte, needle string) bool {
	rest := path
	if nextHFSRune(&rest) != '.' {
		return false
	}
	for i := range len(needle) {
		c := nextHFSRune(&rest)
		if c > 127 || rune(lower(byte(c))) != rune(needle[i]) {
			return false
		}
	}
	c := nextHFSRune(&rest)
	return c == 0 || c == '/'
}
