package pluginregistry

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"actionlint.kjanat.dev/v2/internal/pluginref"
	"golang.org/x/mod/semver"
)

const maxJSONBytes = 4 << 20

// Client reads bounded static JSON. It never executes a plugin or treats a
// compatibility report for another host revision as proof for this host.
type Client struct {
	StatusOptions StatusOptions
	base          *url.URL
	http          *http.Client
}

func registryURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err == nil && u.Scheme == "file" {
		if _, err := pluginref.LocalRegistryDirectory(raw); err != nil {
			return nil, err
		}
		u.Path = strings.TrimSuffix(path.Clean(u.Path), "/") + "/"
		u.RawPath = ""
		return u, nil
	}
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || strings.Contains(u.Path, "\\") {
		return nil, errors.New("registry must be an HTTPS directory URL without credentials, query or fragment")
	}
	u.Path = strings.TrimSuffix(path.Clean("/"+strings.TrimPrefix(u.Path, "/")), "/") + "/"
	u.RawPath = ""
	return u, nil
}

func New(raw string) (*Client, error) {
	u, err := registryURL(raw)
	if err != nil {
		return nil, err
	}
	c := &Client{base: u}
	c.http = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !c.contains(req.URL) {
			return errors.New("registry redirect leaves catalogue or exceeds limit")
		}
		return nil
	}}
	return c, nil
}

func (c *Client) contains(u *url.URL) bool {
	return u.Scheme == c.base.Scheme && sameRegistryHost(u, c.base) && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.Contains(u.Path, "\\") && strings.HasPrefix(path.Clean(u.Path), c.base.Path)
}

func sameRegistryHost(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		return "443"
	}
	if port(a) != port(b) {
		return false
	}
	if address, err := netip.ParseAddr(a.Hostname()); err == nil {
		other, err := netip.ParseAddr(b.Hostname())
		return err == nil && address == other
	}
	return strings.EqualFold(a.Hostname(), b.Hostname())
}

func get[T *Catalog | *manifest | *record | *StatusSnapshot](ctx context.Context, c *Client, path string, target T) error {
	u := c.base.ResolveReference(&url.URL{Path: path})
	if !c.contains(u) {
		return errors.New("registry link leaves catalogue")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registry %s: HTTP %d", u, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxJSONBytes {
		return errors.New("registry JSON exceeds 4 MiB")
	}
	// json/v2 rejects duplicate keys and trailing documents, while permitting
	// additive registry fields so catalogue evolution need not break clients.
	if err := jsonv2.Unmarshal(data, target); err != nil {
		return fmt.Errorf("registry %s: %w", u, err)
	}
	return nil
}

func (c *Client) Search(ctx context.Context, query string) ([]CatalogPlugin, error) {
	if c.base.Scheme == "file" {
		return c.searchCandidates(ctx, query)
	}
	var index Catalog
	if err := get(ctx, c, "index.json", &index); err != nil {
		return nil, err
	}
	if index.SchemaVersion != 1 || index.Plugins == nil || index.Source != "" {
		return nil, errors.New("unsupported registry catalogue")
	}
	seen := map[string]bool{}
	aliases := map[string]bool{}
	result := []CatalogPlugin{}
	query = strings.ToLower(query)
	for _, p := range index.Plugins {
		key := strings.ToLower(p.Name)
		parts := strings.Split(p.Manifest, "/")
		if !canonicalName(p.Name) || !idPattern.MatchString(p.PluginID) || seen[key] || len(parts) != 3 || parts[0] != "plugins" || !digestPattern.MatchString(parts[1]) || parts[2] != "index.json" {
			return nil, fmt.Errorf("invalid or duplicate catalogue plugin %q", p.Name)
		}
		seen[key] = true
		matchesAlias := false
		for _, alias := range p.Aliases {
			if !pluginref.ValidAlias(alias) || aliases[alias] {
				return nil, fmt.Errorf("invalid or duplicate catalogue alias %q", alias)
			}
			aliases[alias] = true
			matchesAlias = matchesAlias || strings.Contains(alias, query)
		}
		if strings.Contains(key, query) || strings.Contains(strings.ToLower(p.PluginID), query) || matchesAlias {
			result = append(result, p)
		}
	}
	slices.SortFunc(result, func(a, b CatalogPlugin) int { return strings.Compare(a.Name, b.Name) })
	return result, nil
}

func (c *Client) manifest(ctx context.Context, name string) (CatalogPlugin, manifest, error) {
	plugins, err := c.Search(ctx, name)
	if err != nil {
		return CatalogPlugin{}, manifest{}, err
	}
	for _, p := range plugins {
		if !strings.EqualFold(p.Name, name) && !slices.Contains(p.Aliases, name) {
			continue
		}
		var m manifest
		if err := get(ctx, c, p.Manifest, &m); err != nil {
			return p, m, err
		}
		i := m.Plugin
		if m.Source != "" || i.SchemaVersion != 1 || i.Issuer == "" || i.Issuer != publisherIssuer(i.Provider) || !numericID(i.RepositoryID) || !numericID(i.OwnerID) || i.Provider+".com/"+i.Repository != p.Name || i.PluginID != p.PluginID || p.Manifest != "plugins/"+hash(i.Issuer+"\n"+i.RepositoryID)+"/index.json" || m.Versions == nil {
			return p, m, errors.New("registry manifest disagrees with catalogue identity")
		}
		seen := map[string]bool{}
		for _, v := range m.Versions {
			if !pluginref.ValidVersion(v.Version) || seen[v.Version] || v.Record != "versions/"+hash(v.Version)+".json" {
				return p, m, errors.New("invalid or duplicate registry version")
			}
			seen[v.Version] = true
		}
		return p, m, nil
	}
	return CatalogPlugin{}, manifest{}, fmt.Errorf("plugin %q not found in registry", name)
}

func (c *Client) Versions(ctx context.Context, name string) ([]Version, error) {
	if c.base.Scheme == "file" {
		_, m, err := c.candidateManifest(ctx, name)
		return m.Versions, err
	}
	_, m, err := c.manifest(ctx, name)
	return m.Versions, err
}

func selectVersion(versions []Version, selector string) (Version, error) {
	if selector != "latest" {
		for _, v := range versions {
			if v.Version == selector {
				return v, nil
			}
		}
		return Version{}, fmt.Errorf("version %q not published", selector)
	}
	var best Version
	ambiguous := false
	for _, v := range versions {
		normalized := "v" + strings.TrimPrefix(v.Version, "v")
		core, _, _ := strings.Cut(normalized, "+")
		if !semver.IsValid(normalized) || strings.Count(core, ".") != 2 || semver.Prerelease(normalized) != "" {
			continue
		}
		comparison := semver.Compare(normalized, "v"+strings.TrimPrefix(best.Version, "v"))
		if best.Version == "" || comparison > 0 {
			best, ambiguous = v, false
		} else if comparison == 0 {
			ambiguous = true
		}
	}
	if best.Version == "" {
		return Version{}, errors.New("no stable semantic version; select an exact tag with --version")
	}
	if ambiguous {
		return Version{}, errors.New("latest version has ambiguous tags; select an exact tag with --version")
	}
	return best, nil
}

func (c *Client) Resolve(ctx context.Context, name, selector string) (Pin, error) {
	if c.base.Scheme == "file" {
		return c.resolveCandidate(ctx, name, selector)
	}
	statuses, err := c.RefreshStatuses(ctx)
	if err != nil {
		return Pin{}, err
	}
	p, m, err := c.manifest(ctx, name)
	if err != nil {
		return Pin{}, err
	}
	versions := slices.Clone(m.Versions)
	if selector == "latest" {
		versions, err = c.availableVersions(ctx, p, m, statuses)
		if err != nil {
			return Pin{}, err
		}
	}
	for {
		v, err := selectVersion(versions, selector)
		if err != nil {
			return Pin{}, err
		}
		pin, err := c.resolveVersion(ctx, p, m, v, selector)
		if err != nil {
			return Pin{}, err
		}
		if pluginref.ValidAlias(name) {
			pin.Alias = name
		}
		err = checkStatus(ctx, pin, statuses, c.StatusOptions, true)
		if selector == "latest" && errors.Is(err, errReleaseUnavailable) {
			versions = slices.DeleteFunc(versions, func(candidate Version) bool { return candidate.Version == v.Version })
			continue
		}
		if err != nil {
			return Pin{}, err
		}
		if err := c.cacheRelease(ctx, pin); err != nil {
			return Pin{}, err
		}
		return pin, nil
	}
}

func (c *Client) availableVersions(ctx context.Context, plugin CatalogPlugin, manifest manifest, statuses StatusSnapshot) ([]Version, error) {
	versions := slices.Clone(manifest.Versions)
	for _, status := range statuses.Releases {
		if !strings.EqualFold(status.Name, plugin.Name) || status.Status != ReleaseWithdrawn && status.Status != ReleaseRevoked {
			continue
		}
		for _, version := range versions {
			if version.Version != status.Version {
				continue
			}
			// Confirm the status binds the published bytes before excluding it.
			pin, err := c.resolveVersion(ctx, plugin, manifest, version, "latest")
			if err != nil {
				return nil, err
			}
			err = checkStatus(ctx, pin, statuses, c.StatusOptions, true)
			if !errors.Is(err, errReleaseUnavailable) {
				return nil, err
			}
			versions = slices.DeleteFunc(versions, func(candidate Version) bool { return candidate.Version == version.Version })
			break
		}
	}
	return versions, nil
}

func (c *Client) resolveVersion(ctx context.Context, p CatalogPlugin, m manifest, v Version, selector string) (Pin, error) {
	var r record
	directory := strings.TrimSuffix(p.Manifest, "index.json")
	if err := get(ctx, c, directory+v.Record, &r); err != nil {
		return Pin{}, err
	}
	d := r.Artifact
	if r.Source != "" {
		return Pin{}, errors.New("HTTPS registries cannot serve local candidates")
	}
	switch {
	case r.RecordVersion == 2 && d != nil && r.Validation == nil:
		if d.SHA256 != r.Submission.SHA256 {
			return Pin{}, errors.New("registry artifact checksum disagrees with submission")
		}
	case r.RecordVersion == 0 && d == nil && r.Validation != nil:
		d = r.Validation
	default:
		return Pin{}, errors.New("unsupported registry version record")
	}
	i, s, ri := m.Plugin, r.Submission, r.Receipt.Identity
	if s.SchemaVersion != 1 || s.Tag != v.Version || s.Provider != i.Provider || s.Repository != i.Repository ||
		r.Receipt.SchemaVersion != 1 || r.Receipt.Submission != s || ri.Issuer != i.Issuer || ri.RepositoryID != i.RepositoryID || ri.OwnerID != i.OwnerID || ri.Repository != i.Repository || ri.Ref != "refs/tags/"+v.Version || ri.Commit != r.Commit ||
		strconv.FormatInt(r.RepositoryID, 10) != i.RepositoryID || strconv.FormatInt(r.OwnerID, 10) != i.OwnerID || d.PluginID != i.PluginID || d.Kind != s.Kind || !strings.EqualFold(d.Repository, "https://"+p.Name) || strings.TrimPrefix(d.PluginVersion, "v") != strings.TrimPrefix(v.Version, "v") {
		return Pin{}, errors.New("registry version disagrees with publisher identity or selected release")
	}
	pin := Pin{Registry: c.base.String(), Name: p.Name, RepositoryID: i.RepositoryID, OwnerID: i.OwnerID, PluginID: i.PluginID, Selector: selector, Version: v.Version, Kind: s.Kind, Asset: s.Asset, URL: r.URL, SHA256: s.SHA256, Contract: d.Contract}
	if len(p.Aliases) == 1 {
		pin.Alias = p.Aliases[0]
	}
	if err := pin.Validate(); err != nil {
		return Pin{}, err
	}
	if err := pin.verifyMetadata(d.Metadata); err != nil {
		return Pin{}, err
	}
	return pin, nil
}
