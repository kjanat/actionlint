package tree

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/address"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
)

// Loaded contains merged values, source files, and source locations by JSON pointer.
type Loaded struct {
	Value   any
	Files   []string
	Origins map[string]Input
}

func Load(path string, read func(string) ([]byte, error), normalize func(string, document.Document) (document.Document, error)) (Loaded, error) {
	return LoadWithMerge(path, read, normalize, Merge)
}

func LoadWithMerge(path string, read func(string) ([]byte, error), normalize func(string, document.Document) (document.Document, error), merge func(any, any) any) (Loaded, error) {
	active := map[string]bool{}
	cache := map[string]Loaded{}
	var files []string
	var load func(string, int) (Loaded, error)
	load = func(path string, depth int) (Loaded, error) {
		readPath := path
		path, err := address.Normalize(path)
		if err != nil {
			return Loaded{}, err
		}
		identity := path
		if !address.Remote(path) {
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
		doc, err := document.Parse(readPath, data)
		if err != nil {
			return Loaded{}, fmt.Errorf("config %q: %w", path, err)
		}
		if normalize != nil {
			doc, err = normalize(path, doc)
			if err != nil {
				return Loaded{}, fmt.Errorf("config %q: %w", path, err)
			}
		}
		var bases []string
		if object, ok := doc.Value.(map[string]any); ok {
			if value, present := object["extends"]; present && value != nil {
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
		}
		result := Loaded{Origins: map[string]Input{}}
		for _, name := range bases {
			resolved, err := address.Resolve(path, name)
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
			MergeOrigins(result.Origins, inherited.Origins, result.Value, inherited.Value)
			result.Value = merge(result.Value, inherited.Value)
		}
		source := map[string]Input{}
		for pointer, loc := range doc.ValueLocations {
			key := doc.Locations[pointer]
			source[pointer] = Input{File: path, Line: loc.Line, Column: loc.Column, KeyLine: key.Line, KeyColumn: key.Column}
		}
		if object, ok := doc.Value.(map[string]any); ok {
			object = maps.Clone(object)
			delete(object, "extends")
			doc.Value = object
		}
		MergeOrigins(result.Origins, source, result.Value, doc.Value)
		result.Value = merge(result.Value, doc.Value)
		cache[path] = result
		files = append(files, path)
		return result, nil
	}
	result, err := load(path, 0)
	result.Files = files
	return result, err
}

// MergeOrigins updates dst to describe Merge(base, value), removing locations
// for values replaced by the overlay and retaining locations for inherited values.
func MergeOrigins(dst, src map[string]Input, base, value any) {
	mergeOrigins(dst, src, base, value, "")
}

func mergeOrigins(dst, src map[string]Input, base, value any, path string) {
	if object, ok := value.(map[string]any); ok {
		old, existed := dst[path]
		if in, found := src[path]; found {
			if existed && base == nil {
				reset := old
				in.Reset = &reset
			} else if existed && old.Reset != nil {
				in.Reset = old.Reset
			}
			dst[path] = in
		}
		baseObject, _ := base.(map[string]any)
		for key, child := range object {
			mergeOrigins(dst, src, baseObject[key], child, document.Path(path, key))
		}
		return
	}
	for key := range dst {
		if key == path || strings.HasPrefix(key, path+"/") {
			delete(dst, key)
		}
	}
	for key, input := range src {
		if key == path || strings.HasPrefix(key, path+"/") {
			dst[key] = input
		}
	}
}

type extendsError struct {
	origin string
	cause  error
}

func (e extendsError) Error() string { return fmt.Sprintf("extends in %q: %s", e.origin, e.cause) }
func (e extendsError) Unwrap() error { return e.cause }
