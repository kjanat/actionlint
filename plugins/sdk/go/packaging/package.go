// Package packaging builds component distributions from the descriptor exported by a Go plugin.
package packaging

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/plugins/sdk/go/bindings/kjanat/actionlint/analyzer"
)

//go:embed analyzer.wit
var analyzerContract string

// Identity identifies the distributed plugin independently of its diagnostic rule.
type Identity struct {
	// ID is the stable registry identity, such as example/insecure-download.
	ID string `json:"id"`
	// Name is the display name shown when inspecting the package.
	Name string `json:"name"`
	// Description explains what the plugin checks.
	Description string `json:"description"`
	// License is an SPDX license identifier.
	License string `json:"license"`
	// Repository is the HTTPS URL of the plugin's source repository.
	Repository string `json:"repository"`
	// Version identifies this plugin release, without referring to the host version.
	Version string `json:"plugin_version"`
}

// Engine identifies the implementation bundled inside the component.
type Engine struct {
	// Name identifies the checker implementation, which can differ from the package ID.
	Name string `json:"name"`
	// Version identifies the bundled checker version.
	Version string `json:"version"`
}

// Capabilities declares the resources and output features used by the plugin.
type Capabilities struct {
	// Filesystem reports whether checking requires access to host files.
	Filesystem bool `json:"filesystem"`
	// Network reports whether checking requires network access.
	Network bool `json:"network"`
	// Fixes reports whether the plugin can return source edits.
	Fixes bool `json:"fixes"`
	// SourceAnnotations reports whether the plugin reads source-annotation imports.
	SourceAnnotations bool `json:"source_annotations"`
	// SourceFiles reports whether the plugin reads the source-files interface.
	SourceFiles bool `json:"source_files"`
}

// Package supplies release metadata and the same descriptor returned by describe.
// Use this from a native packaging entry point excluded from the Wasm build.
type Package struct {
	// Identity supplies the stable ID and release information written to metadata.json.
	Identity Identity
	// Engine identifies the checker bundled in this release.
	Engine Engine
	// Languages lists the script languages checked by the component.
	Languages []string
	// Capabilities declares requirements; it does not grant host access.
	Capabilities Capabilities
	// Descriptor must be the descriptor returned by the component's describe export.
	Descriptor analyzer.Descriptor
	// LicenseText is the complete license written to the distribution's LICENSE file.
	LicenseText string
}

// Metadata serializes the discovery section without executing a component.
func (p Package) Metadata() ([]byte, error) {
	if len(p.Identity.ID) == 0 || len(p.Identity.ID) > 128 || strings.ContainsFunc(p.Identity.ID, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._/-", r))
	}) {
		return nil, errors.New("plugin ID requires 1 to 128 ASCII letters, digits, '.', '_', '/' or '-'")
	}
	d := p.Descriptor
	inputs := make([]string, 0, d.RequiredInputs.Len())
	for _, input := range d.RequiredInputs.Slice() {
		if input > analyzer.InputKindDependencies {
			return nil, fmt.Errorf("unknown required input kind %d", input)
		}
		inputs = append(inputs, input.String())
	}
	descriptor := map[string]any{
		"name": d.Name, "description": d.Description, "category": d.Category,
		"subcategory": d.Subcategory, "maturity": d.Maturity, "provider": d.Provider,
		"recommended": d.Recommended, "explanation": d.Explanation, "help": d.Help,
		"documentation_url": d.DocumentationURL, "required_inputs": inputs,
	}
	if d.OptionsSchema != "" {
		var schema map[string]json.RawMessage
		if err := json.Unmarshal([]byte(d.OptionsSchema), &schema); err != nil || schema == nil {
			return nil, errors.New("descriptor options schema must be a JSON object")
		}
		descriptor["options_schema"] = json.RawMessage(d.OptionsSchema)
	}
	if examples := d.Examples.Slice(); len(examples) != 0 {
		descriptor["examples"] = examples
	}
	contract := strings.ReplaceAll(analyzerContract, "\r\n", "\n")
	declaration, _, _ := strings.Cut(contract, ";")
	packageName, version, found := strings.Cut(strings.TrimPrefix(declaration, "package "), "@")
	if !found {
		return nil, errors.New("SDK analyzer contract has no package version")
	}
	metadata := struct {
		Identity
		MetadataVersion int            `json:"metadata_version"`
		Transport       string         `json:"transport"`
		Interface       string         `json:"interface"`
		ContractSHA256  string         `json:"contract_sha256"`
		Engine          Engine         `json:"engine"`
		Languages       []string       `json:"languages"`
		Capabilities    Capabilities   `json:"capabilities"`
		Descriptor      map[string]any `json:"descriptor"`
	}{p.Identity, 1, "wasm-component", packageName + "/analyzer@" + version, fmt.Sprintf("%x", sha256.Sum256([]byte(contract))), p.Engine, p.Languages, p.Capabilities, descriptor}
	data, err := json.MarshalIndent(metadata, "", "  ")
	return append(data, '\n'), err
}

// Write creates plugin.wasm, metadata.json, schema.json, LICENSE and SHA256SUMS
// in outputDirectory. Validate the resulting component with wasm-tools before publishing it.
func (p Package) Write(input, outputDirectory string) error {
	metadata, err := p.Metadata()
	if err != nil {
		return err
	}
	component, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	component, err = WithMetadata(component, metadata)
	if err != nil {
		return err
	}
	schema := p.Descriptor.OptionsSchema
	if schema == "" {
		schema = "{}"
	}
	files := []struct {
		name string
		data []byte
	}{
		{"plugin.wasm", component},
		{"metadata.json", metadata},
		{"schema.json", []byte(schema + "\n")},
		{"LICENSE", []byte(strings.ReplaceAll(p.LicenseText, "\r\n", "\n"))},
	}
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return err
	}
	var checksums strings.Builder
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(outputDirectory, file.name), file.data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(&checksums, "%x  %s\n", sha256.Sum256(file.data), file.name)
	}
	return os.WriteFile(filepath.Join(outputDirectory, "SHA256SUMS"), []byte(checksums.String()), 0o644)
}

// WithMetadata replaces the outer actionlint.plugin section. Nested component
// and module bytes are copied unchanged. This checks section framing, not Wasm
// instructions or component types; use wasm-tools validate for those checks.
func WithMetadata(component, metadata []byte) ([]byte, error) {
	if len(component) < 8 || !bytes.Equal(component[:8], []byte{0, 'a', 's', 'm', 13, 0, 1, 0}) {
		return nil, errors.New("input must be a WebAssembly component")
	}
	var object map[string]json.RawMessage
	if len(metadata) > 1<<20 || json.Unmarshal(metadata, &object) != nil || object == nil {
		return nil, errors.New("metadata must be a JSON object within 1 MiB")
	}
	const name = "actionlint.plugin"
	payload := binary.AppendUvarint(nil, uint64(len(name)))
	payload = append(payload, name...)
	payload = append(payload, metadata...)
	output := append([]byte(nil), component[:8]...)
	output = append(output, 0)
	output = binary.AppendUvarint(output, uint64(len(payload)))
	output = append(output, payload...)
	for rest := component[8:]; len(rest) != 0; {
		section := rest
		id := rest[0]
		size, n := binary.Uvarint(rest[1:])
		if n <= 0 || n > 5 || size > uint64(len(rest)-1-n) || size > 1<<32-1 {
			return nil, errors.New("invalid component section length")
		}
		body := rest[1+n : 1+n+int(size)]
		rest = rest[1+n+int(size):]
		if id == 0 {
			length, count := binary.Uvarint(body)
			if count <= 0 || count > 5 || length > uint64(len(body)-count) || length > 1<<32-1 {
				return nil, errors.New("invalid custom section name")
			}
			if string(body[count:count+int(length)]) == name {
				continue
			}
		}
		output = append(output, section[:len(section)-len(rest)]...)
	}
	return output, nil
}
