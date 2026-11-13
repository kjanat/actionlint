package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/input/configuration/address"
	"actionlint.kjanat.dev/v2/internal/input/configuration/document"
	configsource "actionlint.kjanat.dev/v2/internal/input/configuration/source"
	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
)

// FileMigration holds a preview and the exact input graph used to prepare it.
// Only Apply writes. Parse, Read, and virtual filesystem APIs never write files.
type FileMigration struct {
	ruleSet RuleSet
	Path    string
	Migration
	snapshots   map[string]fileSnapshot
	mode        os.FileMode
	connections acquisitionhttp.Connections
}

type fileSnapshot interface {
	bytes() []byte
	configSnapshot()
}

type localConfigSnapshot struct {
	content []byte
	target  string
}

type remoteConfigSnapshot struct{ content []byte }

func (s localConfigSnapshot) bytes() []byte  { return s.content }
func (s remoteConfigSnapshot) bytes() []byte { return s.content }
func (localConfigSnapshot) configSnapshot()  {}
func (remoteConfigSnapshot) configSnapshot() {}

// PrepareMigration reads the selected file and its inherited inputs, validates
// the converted graph, and returns a write candidate tied to those inputs.
// Reads stop when ctx is canceled.
func (ruleSet RuleSet) PrepareMigration(ctx context.Context, path string, connections acquisitionhttp.Connections, registrations ...rule.Descriptor) (*FileMigration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absolute, err := address.Normalize(path)
	if err != nil {
		return nil, err
	}
	if address.Remote(absolute) {
		return nil, errors.New("remote config sources are read-only; select a local file to migrate")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	absolute = filepath.Join(parent, filepath.Base(absolute))
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("cannot migrate non-regular config file %q", absolute)
	}
	snapshots := map[string]fileSnapshot{}
	reader := configsource.New(ctx, nil, connections)
	read := func(path string) ([]byte, error) {
		content, err := reader.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if address.Remote(path) {
			snapshots[path] = remoteConfigSnapshot{content: bytes.Clone(content)}
		} else {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil, err
			}
			snapshots[path] = localConfigSnapshot{content: bytes.Clone(content), target: target}
		}
		return content, err
	}
	normalize := func(origin string, parsed document.Document) (document.Document, error) {
		node := parsed.Value
		if err := ValidateSourcePaths(origin, node); err != nil {
			return document.Document{}, err
		}
		resolved, err := ruleSet.resolveValue(node, documentInputs(parsed), false)
		parsed.Value = resolved.Value
		return parsed, err
	}
	before, err := loadConfigTree(absolute, read, normalize)
	if err != nil {
		return nil, err
	}
	migration, err := ruleSet.MigrateDocument(absolute, snapshots[absolute].bytes(), registrations...)
	if err != nil {
		return nil, err
	}
	// Validate in the same graph, including plugin rule settings without loading
	// any guest. Dependency identity is unchanged by config syntax migration.
	after, err := loadConfigTree(absolute, func(path string) ([]byte, error) {
		if path == absolute {
			return migration.Content, nil
		}
		content, exists := snapshots[path]
		if !exists {
			return nil, fmt.Errorf("migration introduced unexpected config dependency %q", path)
		}
		return content.bytes(), nil
	}, normalize)
	if err != nil {
		return nil, err
	}
	effective := func(node any) (map[string]any, error) {
		// Compare the merged graphs in the same vocabulary. Comparing raw
		// category mappings would reject every intentional category migration.
		canonical, _, err := ruleSet.migrateRuleCategories(node, registrations)
		if err != nil {
			return nil, err
		}
		resolved, err := ruleSet.resolveValue(canonical, nil, false)
		if err != nil {
			return nil, err
		}
		return ruleSet.EffectiveValues(resolved.Config)
	}
	beforeValues, err := effective(before.Value)
	if err != nil {
		return nil, err
	}
	afterValues, err := effective(after.Value)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(beforeValues, afterValues) {
		return nil, errors.New("migration cannot preserve inherited settings in this format; convert the selected config to YAML or JSON first")
	}
	return &FileMigration{Path: absolute, Migration: migration, snapshots: snapshots, mode: info.Mode().Perm(), ruleSet: ruleSet, connections: connections}, nil
}

// SetContent validates an enriched candidate, such as default plugin insertion,
// against the original dependency graph without executing plugins.
func (m *FileMigration) SetContent(content []byte) error {
	ruleSet := m.ruleSet

	if _, err := loadConfigTree(m.Path, func(path string) ([]byte, error) {
		if path == m.Path {
			return content, nil
		}
		original, exists := m.snapshots[path]
		if !exists {
			return nil, fmt.Errorf("migration introduced unexpected config dependency %q", path)
		}
		return original.bytes(), nil
	}, func(origin string, parsed document.Document) (document.Document, error) {
		node := parsed.Value
		if err := ValidateSourcePaths(origin, node); err != nil {
			return document.Document{}, err
		}
		resolved, err := ruleSet.resolveValue(node, documentInputs(parsed), false)
		parsed.Value = resolved.Value
		return parsed, err
	}); err != nil {
		return err
	}
	m.Content = bytes.Clone(content)
	m.Changed = !bytes.Equal(content, m.snapshots[m.Path].bytes())
	return nil
}

// CheckSources compares another previously captured source graph with this
// preview's exact inputs. It performs no acquisition and exposes no snapshots.
func (m *FileMigration) CheckSources(sources map[string][]byte) error {
	if len(sources) != len(m.snapshots) {
		return errors.New("config source graph changed during migration preparation; prepare the migration again")
	}
	for path, expected := range m.snapshots {
		content, exists := sources[path]
		original := expected.bytes()
		if !exists || (content == nil) != (original == nil) || !bytes.Equal(content, original) {
			return fmt.Errorf("config input changed during migration preparation: %s; prepare the migration again", path)
		}
	}
	return nil
}

// Apply atomically replaces the selected file after checking the complete
// snapshot. A sibling exclusive lock serializes cooperating migrations;
// external edits detected since preparation are never overwritten.
func (m *FileMigration) Apply(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.SetContent(m.Content); err != nil {
		return err
	}
	if !m.Changed {
		return nil
	}
	if err := m.CheckUnchanged(ctx); err != nil {
		return err
	}
	lockPath := m.Path + ".migrate.lock"
	if err := os.Mkdir(lockPath, 0700); err != nil {
		return fmt.Errorf("cannot lock config migration %q: %w", m.Path, err)
	}
	defer os.Remove(lockPath)
	temp, err := os.CreateTemp(filepath.Dir(m.Path), ".actionlint-migrate-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(m.mode); err != nil {
		temp.Close()
		return err
	}
	_, writeErr := temp.Write(m.Content)
	if err := errors.Join(writeErr, temp.Sync(), temp.Close()); err != nil {
		return err
	}
	if err := m.CheckUnchanged(ctx); err != nil {
		return err
	}
	return os.Rename(temp.Name(), m.Path)
}

// CheckUnchanged verifies that the selected file and inherited inputs still
// match this preview. Orchestrators use it when preparing a multi-file update.
func (m *FileMigration) CheckUnchanged(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(m.Path))
	if err != nil {
		return err
	}
	if parent != filepath.Dir(m.Path) {
		return errors.New("config directory changed before migration; prepare the migration again")
	}
	reader := configsource.New(ctx, nil, m.connections)
	for path, expected := range m.snapshots {
		if _, remote := expected.(remoteConfigSnapshot); remote {
			current, err := reader.ReadFile(path)
			if err != nil {
				return err
			}
			if !bytes.Equal(current, expected.bytes()) {
				return fmt.Errorf("config input changed before migration: %s; prepare the migration again", path)
			}
			continue
		}
		local, ok := expected.(localConfigSnapshot)
		if !ok {
			return fmt.Errorf("invalid config snapshot for %q", path)
		}
		readPath := path
		if path == m.Path {
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("config input changed before migration: %s is not a regular file", path)
			}
			if info.Mode().Perm() != m.mode {
				return errors.New("config permissions changed before migration; prepare the migration again")
			}
		} else {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("config input changed before migration: %s; prepare the migration again: %w", path, err)
			}
			if filepath.Clean(target) != filepath.Clean(local.target) {
				return fmt.Errorf("config input changed before migration: %s now resolves to %s; prepare the migration again", path, target)
			}
			info, err := os.Lstat(target)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("config input changed before migration: %s is not a regular file", path)
			}
			readPath = target
		}
		current, err := reader.ReadFile(readPath)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, expected.bytes()) {
			return fmt.Errorf("config input changed before migration: %s; prepare the migration again", path)
		}
	}
	return nil
}
