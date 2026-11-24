package document

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configsource"
)

// Loaded is a configuration file merged over the files it extends.
// Files lists every source file in merge order, ending with the selected file.
// Contents maps the spelling each source was requested by to its bytes.
type Loaded struct {
	Root     *Node
	Files    []string
	Contents map[string][]byte
	Selected *Editor
}

// Normalizer rewrites one parsed source before it is merged. origin is the
// source's normalized address; the node's locations still name the spelling
// that was read.
type Normalizer func(origin string, source *Node) (*Node, error)

// Files holds parsed configuration sources by normalized address.
type Files struct {
	mu      sync.Mutex
	sources map[string]heldSource
}

type heldSource struct {
	data []byte
	root *Node
}

func NewFiles() *Files { return &Files{sources: map[string]heldSource{}} }

// Add records root as the already loaded source at address, which must be normalized.
func (f *Files) Add(address string, root *Node) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sources[address] = heldSource{root: cloneNode(root)}
}

func (f *Files) parse(address, readPath string, read func(string) ([]byte, error)) ([]byte, *Node, error) {
	if f != nil {
		f.mu.Lock()
		held, found := f.sources[address]
		f.mu.Unlock()
		if found {
			root := cloneNode(held.root)
			root.setFile(readPath)
			return bytes.Clone(held.data), root, nil
		}
	}
	data, err := read(readPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read config %q: %w", address, err)
	}
	parsed, err := ParseConfigDocument(readPath, data)
	if err != nil {
		return nil, nil, fmt.Errorf("config %q: %w", address, err)
	}
	if f != nil {
		f.mu.Lock()
		f.sources[address] = heldSource{data: bytes.Clone(data), root: cloneNode(parsed)}
		f.mu.Unlock()
	}
	return data, parsed, nil
}

// LoadConfigDocument merges path over the files it extends, depth first, according to
// plan. Within one load each file is read and parsed once.
func LoadConfigDocument(path string, files *Files, read func(string) ([]byte, error), normalize Normalizer, plan Plan) (Loaded, error) {
	active := map[string]bool{}
	cache := map[string]Loaded{}
	var order []string
	contents := map[string][]byte{}
	var selected *Editor
	var load func(string, int) (Loaded, error)
	load = func(path string, depth int) (Loaded, error) {
		readPath := path
		path, err := configsource.NormalizeAddress(path)
		if err != nil {
			return Loaded{}, err
		}
		identity := path
		if !configsource.IsRemoteAddress(path) {
			if actual, e := filepath.EvalSymlinks(path); e == nil {
				identity = actual
			}
		}
		if active[identity] {
			return Loaded{}, fmt.Errorf("cyclic config extends at %q", path)
		}
		if depth > 32 {
			return Loaded{}, fmt.Errorf("config extends exceeds 32 levels at %q", path)
		}
		if cached, ok := cache[path]; ok {
			return cached, nil
		}
		active[identity] = true
		defer delete(active, identity)
		data, parsed, err := files.parse(path, readPath, read)
		if err != nil {
			return Loaded{}, err
		}
		contents[readPath] = data
		if depth == 0 {
			selected = &Editor{path: readPath, syntax: syntaxOf(readPath), original: slices.Clone(data), root: cloneNode(parsed)}
		}
		if normalize != nil {
			parsed, err = normalize(path, parsed)
			if err != nil {
				return Loaded{}, fmt.Errorf("config %q: %w", path, err)
			}
		}
		parsed = cloneNode(parsed)
		parsed.setFile(path)
		var bases []string
		if extends := parsed.Child("extends"); extends != nil {
			if value := extends.Value(); value != nil {
				list, ok := value.([]any)
				if !ok {
					return Loaded{}, fmt.Errorf("config %q extends must be a list of strings", path)
				}
				for _, entry := range list {
					name, ok := entry.(string)
					if !ok {
						return Loaded{}, fmt.Errorf("config %q extends entries must be strings", path)
					}
					bases = append(bases, name)
				}
			}
			delete(parsed.object, "extends")
		}
		var result Loaded
		for _, name := range bases {
			resolved, err := configsource.ResolveAddress(path, name)
			if err != nil {
				return Loaded{}, fmt.Errorf("extends in %q: %w", path, err)
			}
			inherited, err := load(resolved, depth+1)
			if err != nil {
				cause := err
				if errors.Is(err, os.ErrNotExist) {
					cause = errors.New(err.Error())
				}
				return Loaded{}, extendsError{origin: path, cause: cause}
			}
			result.Root = Merge(result.Root, inherited.Root, plan)
		}
		result.Root = Merge(result.Root, parsed, plan)
		cache[path] = result
		order = append(order, path)
		return result, nil
	}
	result, err := load(path, 0)
	result.Files, result.Contents, result.Selected = order, contents, selected
	return result, err
}

type extendsError struct {
	origin string
	cause  error
}

func (e extendsError) Error() string { return fmt.Sprintf("extends in %q: %s", e.origin, e.cause) }
func (e extendsError) Unwrap() error { return e.cause }
