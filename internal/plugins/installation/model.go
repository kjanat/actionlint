// Package installation resolves plugin declarations against registry catalogues.
package installation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	pluginref "actionlint.kjanat.dev/v2/internal/plugins/manifests/references"
)

const DefaultURL = "https://plugins.actionlint.kjanat.dev/"

var (
	namePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,128}$`)
	digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type Catalog struct {
	SchemaVersion int             `json:"schema_version"`
	Source        string          `json:"source,omitempty"`
	Plugins       []CatalogPlugin `json:"plugins"`
}

type CatalogPlugin struct {
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases,omitempty"`
	PluginID string   `json:"plugin_id"`
	Manifest string   `json:"manifest"`
}

type Compatibility struct {
	Host struct {
		Name     string `json:"name"`
		Revision string `json:"revision"`
	} `json:"host"`
	Policy  string `json:"policy"`
	Outcome string `json:"outcome"`
	Report  string `json:"report"`
}

type Version struct {
	Version       string          `json:"version"`
	Record        string          `json:"record"`
	Compatibility []Compatibility `json:"compatibility"`
}

type identity struct {
	SchemaVersion int    `json:"schema_version"`
	Issuer        string `json:"issuer"`
	RepositoryID  string `json:"repository_id"`
	OwnerID       string `json:"owner_id"`
	Provider      string `json:"provider"`
	Repository    string `json:"repository"`
	PluginID      string `json:"plugin_id"`
}

type manifest struct {
	Source   string    `json:"source,omitempty"`
	Plugin   identity  `json:"plugin"`
	Versions []Version `json:"versions"`
}

type submission struct {
	SchemaVersion int            `json:"schema_version"`
	Provider      string         `json:"provider"`
	Repository    string         `json:"repository"`
	Tag           string         `json:"tag"`
	Asset         string         `json:"asset,omitempty"`
	SHA256        string         `json:"sha256,omitempty"`
	Kind          artifacts.Kind `json:"kind"`
	Artifact      distribution   `json:"artifact,omitzero"`
}

type declaration struct {
	Kind          artifacts.Kind  `json:"kind"`
	SHA256        string          `json:"sha256"`
	PluginID      string          `json:"plugin_id"`
	PluginVersion string          `json:"plugin_version"`
	Repository    string          `json:"repository"`
	Contract      string          `json:"contract"`
	Metadata      json.RawMessage `json:"metadata"`
}

type record struct {
	IdentityVersion int          `json:"identity_version,omitempty"`
	Source          string       `json:"source,omitempty"`
	RecordVersion   int          `json:"record_version"`
	Artifact        *declaration `json:"artifact"`
	Validation      *declaration `json:"validation"`
	Submission      submission   `json:"submission"`
	Receipt         struct {
		SchemaVersion int        `json:"schema_version"`
		Submission    submission `json:"submission"`
		Identity      struct {
			Issuer       string `json:"issuer"`
			RepositoryID string `json:"repository_id"`
			OwnerID      string `json:"owner_id"`
			Repository   string `json:"repository"`
			Ref          string `json:"ref"`
			Commit       string `json:"commit"`
		} `json:"identity"`
	} `json:"receipt"`
	OwnerID      int64  `json:"owner_id"`
	RepositoryID int64  `json:"repository_id"`
	Commit       string `json:"commit"`
	URL          string `json:"url"`
}

// Pin records selection intent separately from the exact artifact. Lint and sync
// use only the latter; only install/update contact the catalogue.
type Pin struct {
	Origin       string         `json:"source,omitempty"`
	Registry     string         `json:"registry"`
	Name         string         `json:"name"`
	Repository   string         `json:"repository,omitempty"`
	Alias        string         `json:"alias,omitempty"`
	RepositoryID string         `json:"repository_id,omitempty"`
	OwnerID      string         `json:"owner_id,omitempty"`
	PluginID     string         `json:"plugin_id"`
	Selector     string         `json:"selector"`
	Version      string         `json:"version"`
	Kind         artifacts.Kind `json:"kind"`
	Asset        string         `json:"asset"`
	URL          string         `json:"url"`
	SHA256       string         `json:"sha256"`
	Contract     string         `json:"contract"`
}

// SameRelease compares immutable release evidence, ignoring alias spelling and
// whether install originally requested latest or the exact release.
func (p Pin) SameRelease(other Pin) bool { return sameRelease(p, other) }

func (p Pin) Source() string {
	if artifacts.IsOCI(p.URL) {
		return p.URL
	}
	if p.Origin == "candidate" {
		return "sha256://" + p.SHA256
	}
	return p.URL + "#sha256=" + p.SHA256
}

// Matches binds declaration intent to the recorded registry and repository.
// An alias matches only when the selected release records that lookup.
func (p Pin) Matches(ref pluginref.Reference) bool {
	if !SameRegistry(p.Registry, ref.Registry()) {
		return false
	}
	if ref.IsAlias() {
		return p.Alias == ref.Name()
	}
	return strings.EqualFold(p.Name, ref.Name())
}

// SameRegistry compares catalogue endpoints with case-insensitive hosts and
// case-sensitive paths.
func SameRegistry(left, right string) bool {
	a, err := registryURL(left)
	if err != nil {
		return false
	}
	b, err := registryURL(right)
	if err != nil {
		return false
	}
	return a.Scheme == b.Scheme && sameRegistryHost(a, b) && a.Path == b.Path
}

func numericID(value string) bool {
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == value
}

func canonicalName(name string) bool {
	return pluginref.ValidName(name)
}

// RepositoryName returns the publishing repository, independently of a package
// selector. Historical pins omit Repository and retain their original Name.
func (p Pin) RepositoryName() string {
	if p.Repository != "" {
		return p.Repository
	}
	return p.Name
}

func (p Pin) validIdentity() bool {
	repository := p.RepositoryName()
	if !pluginref.ValidRepository(repository) {
		return false
	}
	return p.Repository == "" || p.Name == strings.ToLower(p.PluginID) && strings.HasPrefix(p.Name, repository+"/")
}

func (p Pin) Validate() error {
	if p.Origin == "candidate" {
		return p.validateCandidate()
	}
	if p.Origin != "" || strings.HasPrefix(p.Registry, "file:") {
		return errors.New("invalid plugin source")
	}
	if _, err := registryURL(p.Registry); err != nil {
		return err
	}
	if !canonicalName(p.Name) || !p.validIdentity() || p.Alias != "" && !pluginref.ValidAlias(p.Alias) || !numericID(p.RepositoryID) || !numericID(p.OwnerID) || !idPattern.MatchString(p.PluginID) ||
		!pluginref.ValidVersion(p.Version) || (p.Selector != "latest" && p.Selector != p.Version) ||
		(p.Kind != artifacts.Core && p.Kind != artifacts.Component && p.Kind != artifacts.ProviderManifest) || !digestPattern.MatchString(p.SHA256) || p.Contract == "" {
		return fmt.Errorf("invalid registry pin for %q", p.Name)
	}
	if artifacts.IsOCI(p.URL) {
		ref, err := artifacts.ParseOCI(p.URL)
		if err != nil || ref.String() != p.URL || ref.Digest() != p.SHA256 || p.Asset != "" {
			return errors.New("invalid registry OCI pin")
		}
		return nil
	}
	if !validAsset(p.Kind, p.Asset) {
		return errors.New("invalid registry release asset")
	}
	var expected string
	if strings.HasPrefix(p.Name, "github.com/") {
		expected = "https://" + p.RepositoryName() + "/releases/download/" + url.PathEscape(p.Version) + "/" + url.PathEscape(p.Asset)
	} else {
		expected = "https://gitlab.com/api/v4/projects/" + p.RepositoryID + "/packages/generic/actionlint-plugin/" + url.PathEscape(p.Version) + "/" + url.PathEscape(p.Asset)
	}
	if p.URL != expected {
		return fmt.Errorf("artifact URL disagrees with publisher and version for %s", p.Name)
	}
	return nil
}

func (p Pin) Verify(info artifacts.Info) error {
	if info.Kind != p.Kind || info.SHA256 != p.SHA256 {
		return errors.New("artifact kind or digest disagrees with registry pin")
	}
	return p.verifyMetadata(info.Metadata)
}

func (p Pin) verifyMetadata(data []byte) error {
	var m struct {
		Version       int    `json:"metadata_version"`
		ID            string `json:"id"`
		PluginVersion string `json:"plugin_version"`
		Repository    string `json:"repository"`
		Transport     string `json:"transport"`
	}
	if err := jsonv2.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("plugin metadata: %w", err)
	}
	if m.Version != 1 || m.ID != p.PluginID || strings.TrimPrefix(m.PluginVersion, "v") != strings.TrimPrefix(p.Version, "v") || !strings.EqualFold(m.Repository, "https://"+p.RepositoryName()) {
		return errors.New("plugin metadata disagrees with registry identity or version")
	}
	contract := ""
	if p.Kind == artifacts.Core && m.Transport == "wasm-core-json" {
		var core struct {
			ABI uint32 `json:"abi_version"`
		}
		if err := jsonv2.Unmarshal(data, &core); err != nil {
			return err
		}
		if core.ABI > 0 {
			contract = m.Transport + "/" + strconv.FormatUint(uint64(core.ABI), 10)
		}
	}
	if p.Kind == artifacts.Component && m.Transport == "wasm-component" {
		var component struct {
			Interface       string `json:"interface"`
			WITSHA256       string `json:"wit_sha256"`
			LegacyWITSHA256 string `json:"contract_sha256"`
		}
		if err := jsonv2.Unmarshal(data, &component); err != nil {
			return err
		}
		if component.WITSHA256 != "" && component.LegacyWITSHA256 != "" && component.WITSHA256 != component.LegacyWITSHA256 {
			return errors.New("plugin metadata declares conflicting WIT checksums")
		}
		// Published releases used contract_sha256 before the field was renamed.
		witSHA256 := component.WITSHA256
		if witSHA256 == "" {
			witSHA256 = component.LegacyWITSHA256
		}
		if component.Interface != "" && digestPattern.MatchString(witSHA256) {
			contract = component.Interface + "#sha256=" + witSHA256
		}
	}
	if p.Kind == artifacts.ProviderManifest && m.Transport == "process" {
		contract = "process/1"
	}
	if contract == "" || contract != p.Contract {
		return errors.New("plugin contract disagrees with registry pin")
	}
	return nil
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func publisherIssuer(provider string) string {
	switch provider {
	case "github":
		return "https://token.actions.githubusercontent.com"
	case "gitlab":
		return "https://gitlab.com"
	default:
		return ""
	}
}
