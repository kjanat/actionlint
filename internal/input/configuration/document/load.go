package document

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration/source"
)

// Loaded is a configuration file merged over the files it extends.
// Files lists every source file in merge order, ending with the selected file.
// Selected holds the selected file as parsed, before normalization, so it can
// be edited without parsing it again.
type Loaded struct {
	Root     *Node
	Files    []string
	Selected *Editor
}

// Normalizer rewrites one parsed source before it is merged. origin is the
// source's normalized address; the node's locations still name the spelling
// that was read. A nil Normalizer keeps each source unchanged.
type Normalizer func(origin string, source *Node) (*Node, error)

// Load reads path and the files it extends, depth first, and merges each
// source over its bases according to plan. Each file is read and parsed once.
func Load(path string, read func(string) ([]byte, error), normalize Normalizer, plan Plan) (Loaded, error) {
	active := map[string]bool{}
	cache := map[string]Loaded{}
	var files []string
	var selected *Editor
	var load func(string, int) (Loaded, error)
	load = func(path string, depth int) (Loaded, error) {
		readPath := path
		path, err := source.NormalizeAddress(path)
		if err != nil {
			return Loaded{}, err
		}
		identity := path
		if !source.IsRemoteAddress(path) {
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
		data, err := read(readPath)
		if err != nil {
			return Loaded{}, fmt.Errorf("read config %q: %w", path, err)
		}
		parsed, err := Parse(readPath, data)
		if err != nil {
			return Loaded{}, fmt.Errorf("config %q: %w", path, err)
		}
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
			resolved, err := source.ResolveAddress(path, name)
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
			result.Root = Overlay(result.Root, inherited.Root, plan)
		}
		result.Root = Overlay(result.Root, parsed, plan)
		cache[path] = result
		files = append(files, path)
		return result, nil
	}
	result, err := load(path, 0)
	result.Files, result.Selected = files, selected
	return result, err
}

// extendsError reports a base that failed to load from the file that extends it.
type extendsError struct {
	origin string
	cause  error
}

func (e extendsError) Error() string { return fmt.Sprintf("extends in %q: %s", e.origin, e.cause) }
func (e extendsError) Unwrap() error { return e.cause }
