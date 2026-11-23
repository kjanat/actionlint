// Package packaging builds component distributions from the exported descriptor of a Go plugin.
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

	"actionlint.kjanat.dev/v2/plugins/sdk/go/bindings/kjanat/actionlint/v0.2.0/analyzer"
)

//go:embed analyzer.wit
var analyzerWIT string

type Identity struct {
	// ID is the stable registry identity, such as example/insecure-download.
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// License is an SPDX license identifier.
	License string `json:"license"`
	// Repository is the HTTPS URL of the plugin's source repository.
	Repository string `json:"repository"`
	// Version identifies this plugin release and is independent of the host version.
	Version string `json:"plugin_version"`
}

type Engine struct {
	// Name identifies the checker implementation, which can differ from the package ID.
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Capabilities struct {
	Filesystem bool `json:"filesystem"`
	Network    bool `json:"network"`
	Fixes      bool `json:"fixes"`
	// SourceAnnotations reports whether the plugin reads source-annotation imports.
	SourceAnnotations bool `json:"source_annotations"`
	// SourceFiles reports whether the plugin reads the source-files interface.
	SourceFiles bool `json:"source_files"`
}

type Package struct {
	Identity  Identity
	Engine    Engine
	Languages []string
	// Capabilities declares requirements and grants no host access.
	Capabilities Capabilities
	// Descriptor must equal the result of the component's describe export.
	Descriptor  analyzer.Descriptor
	LicenseText string
}

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
	contract := strings.ReplaceAll(analyzerWIT, "\r\n", "\n")
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
		WITSHA256       string         `json:"wit_sha256"`
		Engine          Engine         `json:"engine"`
		Languages       []string       `json:"languages"`
		Capabilities    Capabilities   `json:"capabilities"`
		Descriptor      map[string]any `json:"descriptor"`
	}{p.Identity, 1, "wasm-component", packageName + "/analyzer@" + version, fmt.Sprintf("%x", sha256.Sum256([]byte(contract))), p.Engine, p.Languages, p.Capabilities, descriptor}
	data, err := json.MarshalIndent(metadata, "", "  ")
	return append(data, '\n'), err
}

// Write creates plugin.wasm, metadata.json, schema.json, LICENSE and SHA256SUMS
// in outputDirectory.
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

// WithMetadata replaces the outer actionlint.plugin section and checks section
// framing only. Use wasm-tools validate to check Wasm instructions and component types.
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
