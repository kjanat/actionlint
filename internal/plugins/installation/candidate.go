package installation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

type candidateManifest struct {
	Source   string    `json:"source"`
	Name     string    `json:"name"`
	PluginID string    `json:"plugin_id"`
	Versions []Version `json:"versions"`
}

type candidateRecord struct {
	Source   string      `json:"source"`
	Artifact declaration `json:"artifact"`
	URL      string      `json:"url"`
}

func candidatePath(name string) bool {
	return fs.ValidPath(name) && name != "." && !strings.ContainsAny(name, "\\:%?#")
}

func readCandidate[T *Catalog | *candidateManifest | *candidateRecord](ctx context.Context, c *Client, name string, target T) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !candidatePath(name) {
		return errors.New("candidate path must stay within its catalogue")
	}
	directory, err := pluginref.LocalRegistryDirectory(c.base.String())
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := openCandidateFile(root, name)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxJSONBytes {
		return errors.New("candidate record must be a regular file of at most 4 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxJSONBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxJSONBytes {
		return errors.New("candidate record exceeds 4 MiB")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return jsonv2.Unmarshal(data, target, jsonv2.RejectUnknownMembers(true))
}

func (c *Client) searchCandidates(ctx context.Context, query string) ([]CatalogPlugin, error) {
	var catalog Catalog
	if err := readCandidate(ctx, c, "index.json", &catalog); err != nil {
		return nil, err
	}
	if catalog.SchemaVersion != 1 || catalog.Source != "candidate" || catalog.Plugins == nil {
		return nil, errors.New("local registry requires a candidate catalogue")
	}
	seen := map[string]bool{}
	result := []CatalogPlugin{}
	query = strings.ToLower(query)
	for _, p := range catalog.Plugins {
		name := strings.ToLower(p.Name)
		if !canonicalName(p.Name) || !idPattern.MatchString(p.PluginID) || seen[name] || len(p.Aliases) != 0 || !candidatePath(p.Manifest) || !strings.HasPrefix(p.Manifest, "plugins/") || !strings.HasSuffix(p.Manifest, "/index.json") {
			return nil, errors.New("invalid or duplicate candidate identity")
		}
		seen[name] = true
		if strings.Contains(name, query) || strings.Contains(strings.ToLower(p.PluginID), query) {
			result = append(result, p)
		}
	}
	slices.SortFunc(result, func(a, b CatalogPlugin) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

func (c *Client) candidateManifest(ctx context.Context, name string) (CatalogPlugin, candidateManifest, error) {
	plugins, err := c.searchCandidates(ctx, name)
	if err != nil {
		return CatalogPlugin{}, candidateManifest{}, err
	}
	for _, p := range plugins {
		if !strings.EqualFold(p.Name, name) {
			continue
		}
		var m candidateManifest
		if err := readCandidate(ctx, c, p.Manifest, &m); err != nil {
			return p, m, err
		}
		if m.Source != "candidate" || m.Name != p.Name || m.PluginID != p.PluginID || m.Versions == nil {
			return p, m, errors.New("candidate manifest disagrees with catalogue")
		}
		seen := map[string]bool{}
		for _, v := range m.Versions {
			if !pluginref.ValidVersion(v.Version) || seen[v.Version] || !candidatePath(v.Record) || !strings.HasPrefix(v.Record, "versions/") || !strings.HasSuffix(v.Record, ".json") || len(v.Compatibility) != 0 {
				return p, m, errors.New("invalid or duplicate candidate version")
			}
			seen[v.Version] = true
		}
		return p, m, nil
	}
	return CatalogPlugin{}, candidateManifest{}, fmt.Errorf("plugin %q not found in candidate catalogue", name)
}

func (c *Client) resolveCandidate(ctx context.Context, name, selector string) (Pin, error) {
	p, m, err := c.candidateManifest(ctx, name)
	if err != nil {
		return Pin{}, err
	}
	v, err := selectVersion(m.Versions, selector)
	if err != nil {
		return Pin{}, err
	}
	var r candidateRecord
	if err := readCandidate(ctx, c, path.Join(path.Dir(p.Manifest), v.Record), &r); err != nil {
		return Pin{}, err
	}
	d := r.Artifact
	repository := strings.ToLower(strings.TrimPrefix(d.Repository, "https://"))
	if r.Source != "candidate" || d.PluginID != p.PluginID || !pluginref.ValidRepository(repository) || (!strings.EqualFold(p.Name, repository) && (p.Name != strings.ToLower(p.PluginID) || !strings.HasPrefix(p.Name, repository+"/"))) || strings.TrimPrefix(d.PluginVersion, "v") != strings.TrimPrefix(v.Version, "v") {
		return Pin{}, errors.New("candidate declaration disagrees with catalogue")
	}
	pin := Pin{Origin: "candidate", Registry: c.base.String(), Name: p.Name, PluginID: p.PluginID, Selector: selector, Version: v.Version, Kind: d.Kind, Asset: path.Base(r.URL), URL: r.URL, SHA256: d.SHA256, Contract: d.Contract}
	if artifacts.IsOCI(r.URL) {
		pin.Asset = ""
	}
	if !strings.EqualFold(p.Name, repository) {
		pin.Repository = repository
	}
	if err := pin.Validate(); err != nil {
		return Pin{}, err
	}
	if err := pin.verifyMetadata(d.Metadata); err != nil {
		return Pin{}, err
	}
	return pin, nil
}

func (p Pin) validateCandidate() error {
	if _, err := pluginref.LocalRegistryDirectory(p.Registry); err != nil {
		return err
	}
	if p.RepositoryID != "" || p.OwnerID != "" || p.Alias != "" || !canonicalName(p.Name) || !p.validIdentity() || !idPattern.MatchString(p.PluginID) || !pluginref.ValidVersion(p.Version) || (p.Selector != "latest" && p.Selector != p.Version) || (p.Kind != artifacts.Core && p.Kind != artifacts.Component && p.Kind != artifacts.ProviderManifest) || !digestPattern.MatchString(p.SHA256) || p.Contract == "" {
		return errors.New("invalid local candidate pin")
	}
	if artifacts.IsOCI(p.URL) {
		ref, err := artifacts.ParseOCI(p.URL)
		if err != nil || ref.String() != p.URL || ref.Digest() != p.SHA256 || p.Asset != "" {
			return errors.New("invalid local candidate OCI pin")
		}
		return nil
	}
	if !candidatePath(p.URL) || p.Asset != path.Base(p.URL) || !validAsset(p.Kind, p.Asset) {
		return errors.New("invalid local candidate artifact path")
	}
	return nil
}

// Installation and sync import candidate bytes into the execution cache.
func (p Pin) importCandidate(ctx context.Context, options artifacts.Options) error {
	if a, err := artifacts.Resolve(ctx, p.Source(), options); err == nil {
		return p.Verify(a.Info)
	} else if artifacts.IsOCI(p.URL) || !errors.Is(err, os.ErrNotExist) {
		return err
	}
	directory, err := pluginref.LocalRegistryDirectory(p.Registry)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := openCandidateFile(root, p.URL)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("candidate artifact must be a regular file")
	}
	return artifacts.Import(ctx, f, p.SHA256, options)
}
