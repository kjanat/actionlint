package pluginregistry

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"strings"

	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
)

// distribution is the closed source union of a v2 publication. The source
// digest identifies distributed bytes, independently of their execution format.
type distribution struct {
	typeName  string
	name      string
	digest    string
	reference string
}

func (d distribution) IsZero() bool { return d.typeName == "" }

func (d distribution) MarshalJSON() ([]byte, error) {
	if d.typeName == "release-asset" {
		return json.Marshal(struct {
			Type   string `json:"type"`
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		}{d.typeName, d.name, d.digest})
	}
	if d.typeName == "oci" {
		return json.Marshal(struct {
			Type      string `json:"type"`
			Reference string `json:"reference"`
		}{d.typeName, d.reference})
	}
	return nil, errors.New("missing publication artifact source")
}

func (d *distribution) UnmarshalJSON(data []byte) error {
	var wire struct {
		Type      string `json:"type"`
		Name      string `json:"name"`
		SHA256    string `json:"sha256"`
		Reference string `json:"reference"`
	}
	if err := jsonv2.Unmarshal(data, &wire, jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := jsonv2.Unmarshal(data, &fields); err != nil {
		return err
	}
	switch wire.Type {
	case "release-asset":
		if len(fields) != 3 || !namePattern.MatchString(wire.Name) || !digestPattern.MatchString(wire.SHA256) || wire.Reference != "" {
			return errors.New("invalid release-asset publication source")
		}
		*d = distribution{typeName: wire.Type, name: wire.Name, digest: wire.SHA256}
	case "oci":
		ref, err := artifact.ParseOCI(wire.Reference)
		if err != nil || len(fields) != 2 || wire.Name != "" || wire.SHA256 != "" || ref.String() != wire.Reference {
			return errors.New("OCI publication source must contain only a normalized digest-pinned reference")
		}
		*d = distribution{typeName: wire.Type, reference: ref.String(), digest: ref.Digest()}
	default:
		return errors.New("unsupported publication artifact source")
	}
	return nil
}

func (s *submission) UnmarshalJSON(data []byte) error {
	type wireSubmission submission
	var wire wireSubmission
	if err := jsonv2.Unmarshal(data, &wire, jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := jsonv2.Unmarshal(data, &fields); err != nil {
		return err
	}
	switch wire.SchemaVersion {
	case 1:
		if len(fields) != 7 {
			return errors.New("legacy publication requires exactly asset and sha256 source fields")
		}
	case 2:
		if len(fields) != 6 {
			return errors.New("v2 publication requires exactly an artifact source")
		}
	default:
		return errors.New("unsupported registry publication schema")
	}
	next := submission(wire)
	if err := next.validateSource(); err != nil {
		return err
	}
	*s = next
	return nil
}

func (s submission) validateSource() error {
	switch s.SchemaVersion {
	case 1:
		if !s.Artifact.IsZero() || !validAsset(s.Kind, s.Asset) || !digestPattern.MatchString(s.SHA256) {
			return errors.New("invalid legacy registry publication source")
		}
	case 2:
		if s.Asset != "" || s.SHA256 != "" || s.Artifact.IsZero() {
			return errors.New("v2 publication requires an artifact source without legacy asset or sha256 fields")
		}
		if s.Artifact.typeName == "release-asset" && !validAsset(s.Kind, s.Artifact.name) {
			return errors.New("publication asset extension disagrees with its format")
		}
	default:
		return errors.New("unsupported registry publication schema")
	}
	return nil
}

func (s submission) sourceDigest() string {
	if s.SchemaVersion == 1 {
		return s.SHA256
	}
	return s.Artifact.digest
}

func (s submission) assetName() string {
	if s.SchemaVersion == 1 {
		return s.Asset
	}
	return s.Artifact.name
}

func (s submission) isOCI() bool { return s.SchemaVersion == 2 && s.Artifact.typeName == "oci" }

func validAsset(kind artifact.Kind, name string) bool {
	if !namePattern.MatchString(name) {
		return false
	}
	switch kind {
	case artifact.Core, artifact.Component:
		return strings.HasSuffix(name, ".wasm") || strings.HasSuffix(name, ".wasm.gz")
	case artifact.ProviderManifest:
		return strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".json.gz")
	default:
		return false
	}
}
