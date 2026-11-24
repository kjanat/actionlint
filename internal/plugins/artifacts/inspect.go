// Package artifacts inspects WebAssembly modules and tool provider packages without compiling or executing them.
package artifacts

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"unicode/utf8"
)

const (
	MetadataSection  = "actionlint.plugin"
	maxMetadataBytes = 1 << 20
)

type ArtifactFormat string

const (
	Core             ArtifactFormat = "core"
	Component        ArtifactFormat = "component"
	ProviderManifest ArtifactFormat = "manifest"
)

// ArtifactProvenance hashes the downloaded bytes before gzip decompression, and its Metadata is untrusted.
type ArtifactProvenance struct {
	Source     string          `json:"source"`
	Kind       ArtifactFormat  `json:"kind"`
	SHA256     string          `json:"sha256"`
	Size       int             `json:"size"`
	ModuleSize int             `json:"module_size"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	// Platform is "<os>/<arch>", or AnyPlatform for a lone image manifest.
	Platform string   `json:"platform,omitempty"`
	Manifest string   `json:"manifest,omitempty"`
	Blobs    []string `json:"blobs,omitempty"`
}

type Artifact struct {
	Info  ArtifactProvenance
	Bytes []byte
}

// SniffPackage checks only package framing and discovery metadata.
func SniffPackage(data []byte) (ArtifactFormat, json.RawMessage, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return Sniff(data)
	}
	metadata, err := ProviderMetadata(data)
	return ProviderManifest, metadata, err
}

func ProviderMetadata(data []byte) (json.RawMessage, error) {
	if len(data) > maxMetadataBytes {
		return nil, errors.New("tool provider manifest exceeds 1 MiB")
	}
	var manifest struct {
		SchemaVersion int             `json:"schema_version"`
		Transport     string          `json:"transport"`
		Metadata      json.RawMessage `json:"metadata"`
	}
	if err := jsonv2.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("tool provider manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 || manifest.Transport != "process" {
		return nil, errors.New("invalid tool provider manifest: discovery requires schema_version 1 and process transport")
	}
	if len(manifest.Metadata) == 0 {
		return bytes.Clone(data), nil
	}
	var object map[string]json.RawMessage
	if err := jsonv2.Unmarshal(manifest.Metadata, &object); err != nil {
		return nil, fmt.Errorf("tool provider discovery metadata: %w", err)
	}
	if object == nil {
		return nil, errors.New("tool provider discovery metadata must be an object")
	}
	return bytes.Clone(manifest.Metadata), nil
}

// Sniff reads only the module envelope and top-level custom sections.
func Sniff(data []byte) (ArtifactFormat, json.RawMessage, error) {
	if len(data) < 8 || !bytes.Equal(data[:4], []byte{0, 'a', 's', 'm'}) {
		return "", nil, errors.New("not a WebAssembly binary")
	}
	var kind ArtifactFormat
	switch string(data[4:8]) {
	case "\x01\x00\x00\x00":
		kind = Core
	case "\x0d\x00\x01\x00":
		kind = Component
	default:
		return "", nil, errors.New("unsupported WebAssembly binary version")
	}
	var metadata json.RawMessage
	for remaining := data[8:]; len(remaining) > 0; {
		id := remaining[0]
		size, rest, err := readU32(remaining[1:])
		if err != nil || uint64(size) > uint64(len(rest)) {
			return "", nil, errors.New("truncated or invalid WebAssembly section")
		}
		section := rest[:size]
		remaining = rest[size:]
		if id != 0 {
			continue
		}
		n, payload, err := readU32(section)
		if err != nil || uint64(n) > uint64(len(payload)) || !utf8.Valid(payload[:n]) {
			return "", nil, errors.New("invalid WebAssembly custom section name")
		}
		if string(payload[:n]) != MetadataSection {
			continue
		}
		if metadata != nil {
			return "", nil, errors.New("duplicate actionlint.plugin custom section")
		}
		payload = payload[n:]
		if len(payload) > maxMetadataBytes {
			return "", nil, errors.New("actionlint.plugin metadata exceeds 1 MiB")
		}
		var object map[string]json.RawMessage
		if err := jsonv2.Unmarshal(payload, &object); err != nil {
			return "", nil, fmt.Errorf("actionlint.plugin metadata: %w", err)
		}
		if object == nil {
			return "", nil, errors.New("actionlint.plugin metadata must be an object")
		}
		metadata = bytes.Clone(payload)
	}
	return kind, metadata, nil
}

func readU32(data []byte) (uint32, []byte, error) {
	var result uint32
	for i := range 5 {
		if i >= len(data) || (i == 4 && data[i] > 15) {
			break
		}
		b := data[i]
		result |= uint32(b&127) << (7 * i)
		if b&128 == 0 {
			return result, data[i+1:], nil
		}
	}
	return 0, nil, errors.New("invalid u32 LEB128")
}

// SameMetadata ignores whitespace and property order and compares numbers without floating-point rounding.
func SameMetadata(a, b []byte) bool {
	x, y := jsontext.Value(bytes.Clone(a)), jsontext.Value(bytes.Clone(b))
	return x.Canonicalize(jsontext.CanonicalizeRawInts(false), jsontext.CanonicalizeRawFloats(false)) == nil && y.Canonicalize(jsontext.CanonicalizeRawInts(false), jsontext.CanonicalizeRawFloats(false)) == nil && bytes.Equal(x, y)
}
