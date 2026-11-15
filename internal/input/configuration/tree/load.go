package tree

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	"actionlint.kjanat.dev/v2/internal/input/configuration/source"
)

// Loaded contains merged values, source files, and source locations by JSON pointer.
type Loaded struct {
	Value   any
	Files   []string
	Origins map[string]Input
	Node    *Node
}

func Load(path string, read func(string) ([]byte, error), normalize func(string, document.Document) (document.Document, error)) (Loaded, error) {
	return LoadWithMerge(path, read, normalize, nil)
}

func LoadWithMerge(path string, read func(string) ([]byte, error), normalize func(string, document.Document) (document.Document, error), plan Plan) (Loaded, error) {
	active := map[string]bool{}
	cache := map[string]Loaded{}
	var files []string
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
			result.Node = Overlay(result.Node, inherited.Node, plan)
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
		result.Node = Overlay(result.Node, Capture(doc.Value, source), plan)
		result.Value, result.Origins = result.Node.Value(), result.Node.Origins()
		cache[path] = result
		files = append(files, path)
		return result, nil
	}
	result, err := load(path, 0)
	result.Files = files
	return result, err
}

type extendsError struct {
	origin string
	cause  error
}

func (e extendsError) Error() string { return fmt.Sprintf("extends in %q: %s", e.origin, e.cause) }
func (e extendsError) Unwrap() error { return e.cause }
