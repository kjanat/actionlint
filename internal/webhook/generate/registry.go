package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"go/format"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/webhook/payload"
)

// These inputs are pinned. Updating them is an explicit maintenance operation;
// ordinary generation and reconciliation never fetch upstream data.
//
//go:embed sources/*
var sources embed.FS

type definition struct {
	OneOf   []string `json:"one-of"`
	Allowed []string `json:"allowed-values"`
	Mapping struct {
		Properties map[string]jsontext.Value `json:"properties"`
	} `json:"mapping"`
	Sequence struct {
		Item string `json:"item-type"`
	} `json:"sequence"`
}

type trigger struct {
	Activities []string
	Fields     []string
}

type sourceManifest struct {
	Sources map[string]struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
	} `json:"sources"`
	Defaults   map[string][]string `json:"defaults"`
	Payload    payload.Provenance  `json:"payload"`
	Exceptions map[string]struct {
		Reason             string `json:"reason"`
		Source             string `json:"source"`
		Scope              string `json:"scope"`
		ProductionVerified *bool  `json:"production_verified"`
	} `json:"exceptions"`
}

func reference(raw jsontext.Value) (string, error) {
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		return name, nil
	}
	var object struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return "", err
	}
	if object.Type == "" {
		return "", errors.New("missing definition reference")
	}
	return object.Type, nil
}

func schemaTriggers(src []byte) (map[string]trigger, error) {
	var schema struct {
		Definitions map[string]definition `json:"definitions"`
	}
	if err := json.Unmarshal(src, &schema); err != nil {
		return nil, err
	}
	defs := schema.Definitions
	on, err := reference(defs["workflow-root-strict"].Mapping.Properties["on"])
	if err != nil || on != "on-strict" || !slices.Contains(defs[on].OneOf, "on-mapping-strict") {
		return nil, errors.New("strict workflow-root-strict -> on-strict -> on-mapping-strict path changed")
	}
	// Walk only references reachable from each strict event definition. Names of
	// individual activity definitions are not inferred from naming conventions.
	walk := func(root string, visit func(definition)) error {
		seen := map[string]bool{}
		var visitRef func(string) error
		visitRef = func(name string) error {
			switch name {
			case "null", "string", "boolean", "number", "any", "sequence", "mapping":
				return nil
			}
			if seen[name] {
				return nil
			}
			seen[name] = true
			d, ok := defs[name]
			if !ok {
				return fmt.Errorf("unknown schema definition %q", name)
			}
			visit(d)
			for _, child := range d.OneOf {
				if err := visitRef(child); err != nil {
					return err
				}
			}
			if d.Sequence.Item != "" {
				return visitRef(d.Sequence.Item)
			}
			return nil
		}
		return visitRef(root)
	}
	result := map[string]trigger{}
	for event, raw := range defs["on-mapping-strict"].Mapping.Properties {
		root, err := reference(raw)
		if err != nil {
			return nil, err
		}
		t := trigger{Activities: []string{}}
		var types jsontext.Value
		if err := walk(root, func(d definition) {
			for field, ref := range d.Mapping.Properties {
				t.Fields = append(t.Fields, field)
				if field == "types" {
					types = ref
				}
			}
		}); err != nil {
			return nil, err
		}
		if types != nil {
			ref, err := reference(types)
			if err != nil {
				return nil, err
			}
			if err := walk(ref, func(d definition) { t.Activities = append(t.Activities, d.Allowed...) }); err != nil {
				return nil, err
			}
			if len(t.Activities) == 0 {
				t.Activities = nil
			} // Free-form dispatch activities.
		}
		slices.Sort(t.Fields)
		t.Fields = slices.Compact(t.Fields)
		slices.Sort(t.Activities)
		t.Activities = slices.Compact(t.Activities)
		result[event] = t
	}
	if len(result) == 0 {
		return nil, errors.New("strict schema contains no triggers")
	}
	return result, nil
}

func documentedTriggers(src, pullRequest []byte) (map[string][]string, error) {
	text := strings.ReplaceAll(string(src), "{% data reusables.actions.workflow-triggers-pull-request-activity-types %}", strings.TrimSpace(string(pullRequest)))
	heading := regexp.MustCompile("^## `([^`]+)`$")
	code := regexp.MustCompile("`([^`]+)`")
	event := ""
	result := map[string][]string{}
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			event = ""
			if match := heading.FindStringSubmatch(line); match != nil {
				event = match[1]
			}
			continue
		}
		if event == "" || !strings.HasPrefix(line, "| ") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 || strings.Contains(cells[1], "Webhook event payload") || strings.Contains(cells[1], "---") {
			continue
		}
		cell := strings.TrimSpace(cells[2])
		if strings.Contains(cell, "{% data ") {
			return nil, fmt.Errorf("unresolved documentation include in %s activity table", event)
		}
		var activities []string
		switch {
		case strings.Contains(cell, "Custom"):
		case cell == "Not applicable":
			activities = []string{}
		default:
			for _, match := range code.FindAllStringSubmatch(cell, -1) {
				activities = append(activities, match[1])
			}
			if len(activities) == 0 {
				return nil, fmt.Errorf("unknown activity table for %s: %s", event, cell)
			}
		}
		slices.Sort(activities)
		result[event] = activities
		event = ""
	}
	if len(result) == 0 {
		return nil, errors.New("documentation contains no trigger tables")
	}
	return result, nil
}

func loadRegistry() (map[string]trigger, sourceManifest, error) {
	return loadRegistryFiles(sources)
}

func loadRegistryFiles(files fs.FS) (map[string]trigger, sourceManifest, error) {
	var manifest sourceManifest
	b, err := fs.ReadFile(files, "sources/manifest.json")
	if err != nil {
		return nil, manifest, err
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		return nil, manifest, err
	}
	if manifest.Payload != payload.Source() {
		return nil, manifest, errors.New("payload provenance changed; update and review the trigger reconciliation manifest")
	}
	inputs := map[string][]byte{}
	for name, source := range manifest.Sources {
		b, err := fs.ReadFile(files, "sources/"+name)
		if err != nil {
			return nil, manifest, err
		}
		if fmt.Sprintf("%x", sha256.Sum256(b)) != source.SHA256 {
			return nil, manifest, fmt.Errorf("pinned source digest mismatch: %s", name)
		}
		inputs[name] = b
	}
	registry, err := schemaTriggers(inputs["workflow-v1.0.json"])
	if err != nil {
		return nil, manifest, err
	}
	docs, err := documentedTriggers(inputs["events-that-trigger-workflows.md"], inputs["workflow-triggers-pull-request-activity-types.md"])
	if err != nil {
		return nil, manifest, err
	}
	differences := map[string]bool{}
	for event, trigger := range registry {
		activities, ok := docs[event]
		if !ok {
			differences["schema-event-only:"+event] = true
			delete(registry, event)
			continue
		}
		for _, activity := range trigger.Activities {
			if !slices.Contains(activities, activity) {
				differences["schema-activity-only:"+event+":"+activity] = true
			}
		}
		for _, activity := range activities {
			if !slices.Contains(trigger.Activities, activity) {
				differences["docs-activity-only:"+event+":"+activity] = true
			}
		}
		if (activities == nil) != (trigger.Activities == nil) {
			differences["custom-activities:"+event] = true
		}
		trigger.Activities = activities
		if len(activities) == 0 && activities != nil {
			trigger.Fields = slices.DeleteFunc(trigger.Fields, func(s string) bool { return s == "types" })
		}
		registry[event] = trigger
	}
	for event := range docs {
		if _, ok := registry[event]; !ok {
			differences["docs-event-only:"+event] = true
		}
	}
	// Payload observations never expand the permitted trigger set. Missing
	// observations and webhook-only activities require reviewed explanations.
	for event, trigger := range registry {
		payloadEvent := event
		if event == "pull_request_target" {
			payloadEvent = "pull_request"
		}
		variants, ok := payload.Variants(payloadEvent)
		if !ok {
			differences["no-payload:"+event] = true
			continue
		}
		if trigger.Activities == nil {
			continue
		} // Arbitrary dispatch values.
		for activity := range variants {
			if activity != "" && !slices.Contains(trigger.Activities, activity) {
				differences["payload-activity-only:"+event+":"+activity] = true
			}
		}
		for _, activity := range trigger.Activities {
			if _, ok := variants[activity]; !ok {
				differences["no-payload-activity:"+event+":"+activity] = true
			}
		}
	}
	var failures []string
	for difference := range differences {
		exception, ok := manifest.Exceptions[difference]
		if !ok {
			failures = append(failures, "unreviewed difference: "+difference)
			continue
		}
		if exception.Reason == "" || exception.Source == "" || exception.Scope == "" || exception.ProductionVerified == nil {
			failures = append(failures, "incomplete exception: "+difference)
		}
	}
	for difference := range manifest.Exceptions {
		if !differences[difference] {
			failures = append(failures, "stale exception: "+difference)
		}
	}
	for event, defaults := range manifest.Defaults {
		trigger, ok := registry[event]
		if !ok || len(defaults) == 0 {
			failures = append(failures, "invalid defaults: "+event)
		}
		for _, activity := range defaults {
			if !slices.Contains(trigger.Activities, activity) {
				failures = append(failures, "default activity is not permitted: "+event+":"+activity)
			}
		}
	}
	if len(failures) > 0 {
		slices.Sort(failures)
		return nil, manifest, errors.New(strings.Join(failures, "\n"))
	}
	return registry, manifest, nil
}

func registrySource() ([]byte, error) {
	registry, manifest, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "// Code generated by actionlint/internal/webhook/generate --registry. DO NOT EDIT.\n\npackage webhook")
	fmt.Fprintln(&buf, "\n// AllWebhookTypes contains permitted Actions activities, not all webhook activities.\n// Sources and reviewed discrepancies: generate/sources/manifest.json.\n// nil denotes unrestricted custom activities; an empty slice denotes no types.\nvar AllWebhookTypes = map[string][]string{")
	writeEntry := func(name string, values []string) {
		if values == nil {
			fmt.Fprintf(&buf, "%q: nil,\n", name)
			return
		}
		fmt.Fprintf(&buf, "%q: {", name)
		for _, value := range values {
			fmt.Fprintf(&buf, "%q,", value)
		}
		fmt.Fprintln(&buf, "},")
	}
	for _, event := range slices.Sorted(maps.Keys(registry)) {
		writeEntry(event, registry[event].Activities)
	}
	fmt.Fprintln(&buf, "}")
	fmt.Fprintln(&buf, "\nvar triggerFields = map[string][]string{")
	for _, event := range slices.Sorted(maps.Keys(registry)) {
		writeEntry(event, registry[event].Fields)
	}
	fmt.Fprintln(&buf, "}")
	fmt.Fprintln(&buf, "\nvar defaultActivities = map[string][]string{")
	for _, event := range slices.Sorted(maps.Keys(manifest.Defaults)) {
		writeEntry(event, manifest.Defaults[event])
	}
	fmt.Fprintln(&buf, "}")
	return format.Source(buf.Bytes())
}

func generateRegistry(dst string) error {
	if !filepath.IsLocal(dst) {
		return fmt.Errorf("output path must be relative: %q", dst)
	}
	src, err := registrySource()
	if err != nil {
		return err
	}
	return os.WriteFile(dst, src, 0o644)
}
