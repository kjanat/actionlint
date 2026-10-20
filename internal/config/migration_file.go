package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	configtree "actionlint.kjanat.dev/internal/config/tree"
	"actionlint.kjanat.dev/internal/fsread"
	"go.yaml.in/yaml/v4"
)

// FileMigration holds a preview and the exact input graph used to prepare it.
// Only Apply writes. Parse, Read, and virtual filesystem APIs never write files.
type FileMigration struct {
	Path string
	Migration
	snapshots map[string][]byte
	mode      os.FileMode
}

// PrepareMigration reads the selected file and its inherited inputs, validates
// the converted graph, and returns a write candidate tied to those inputs.
func PrepareMigration(path string) (*FileMigration, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
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
	snapshots := map[string][]byte{}
	reader := fsread.New(context.Background(), nil, nil)
	read := func(path string) ([]byte, error) {
		content, err := reader.ReadFile(path)
		if err == nil {
			snapshots[path] = bytes.Clone(content)
		}
		return content, err
	}
	normalize := func(_ string, node *yaml.Node) (*yaml.Node, error) {
		resolved, err := resolveNode(node, nil, false)
		return resolved.Node, err
	}
	before, err := configtree.Load(absolute, read, normalize)
	if err != nil {
		return nil, err
	}
	migration, err := MigrateDocument(absolute, snapshots[absolute])
	if err != nil {
		return nil, err
	}
	// Validate in the same graph, including plugin rule settings without loading
	// any guest. Dependency identity is unchanged by config syntax migration.
	after, err := configtree.Load(absolute, func(path string) ([]byte, error) {
		if path == absolute {
			return migration.Content, nil
		}
		content, exists := snapshots[path]
		if !exists {
			return nil, fmt.Errorf("migration introduced unexpected config dependency %q", path)
		}
		return content, nil
	}, normalize)
	if err != nil {
		return nil, err
	}
	effective := func(node *yaml.Node) (map[string]any, error) {
		resolved, err := resolveNode(node, nil, false)
		if err != nil {
			return nil, err
		}
		return EffectiveValues(resolved.Config)
	}
	beforeValues, err := effective(before.Node)
	if err != nil {
		return nil, err
	}
	afterValues, err := effective(after.Node)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(beforeValues, afterValues) {
		return nil, errors.New("migration cannot preserve inherited settings in this format; convert the selected config to YAML or JSON first")
	}
	return &FileMigration{Path: absolute, Migration: migration, snapshots: snapshots, mode: info.Mode().Perm()}, nil
}

// SetContent validates an enriched candidate, such as default plugin insertion,
// against the original dependency graph without executing plugins.
func (m *FileMigration) SetContent(content []byte) error {
	if _, err := configtree.Load(m.Path, func(path string) ([]byte, error) {
		if path == m.Path {
			return content, nil
		}
		original, exists := m.snapshots[path]
		if !exists {
			return nil, fmt.Errorf("migration introduced unexpected config dependency %q", path)
		}
		return original, nil
	}, func(_ string, node *yaml.Node) (*yaml.Node, error) {
		resolved, err := resolveNode(node, nil, false)
		return resolved.Node, err
	}); err != nil {
		return err
	}
	m.Content = bytes.Clone(content)
	m.Changed = !bytes.Equal(content, m.snapshots[m.Path])
	return nil
}

// Apply atomically replaces the selected file after checking the complete
// snapshot. A sibling exclusive lock serializes cooperating migrations;
// external edits detected since preparation are never overwritten.
func (m *FileMigration) Apply() error {
	if err := m.SetContent(m.Content); err != nil {
		return err
	}
	if !m.Changed {
		return nil
	}
	if err := m.CheckUnchanged(); err != nil {
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
	if err := m.CheckUnchanged(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), m.Path)
}

// CheckUnchanged verifies that the selected file and inherited inputs still
// match this preview. Orchestrators use it when preparing a multi-file update.
func (m *FileMigration) CheckUnchanged() error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(m.Path))
	if err != nil {
		return err
	}
	if parent != filepath.Dir(m.Path) {
		return errors.New("config directory changed before migration; prepare the migration again")
	}
	reader := fsread.New(context.Background(), nil, nil)
	for path, expected := range m.snapshots {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("config input changed before migration: %s is not a regular file", path)
		}
		if path == m.Path && info.Mode().Perm() != m.mode {
			return errors.New("config permissions changed before migration; prepare the migration again")
		}
		current, err := reader.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, expected) {
			return fmt.Errorf("config input changed before migration: %s; prepare the migration again", path)
		}
	}
	return nil
}
