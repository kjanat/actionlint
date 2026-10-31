package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

const (
	beginMarker = "# BEGIN GENERATED LAYER TABLE. Edit tools/layers/layers.toml and run go run ./tools/layers."
	endMarker   = "# END GENERATED LAYER TABLE"
)

// render writes rules in the layout the repository's TOML formatter keeps:
// aligned keys and one array element per line.
func render(rules []rule) string {
	var b strings.Builder
	for i, r := range rules {
		if i > 0 {
			b.WriteString("\n")
		}
		for _, line := range r.comment {
			b.WriteString(line + "\n")
		}
		fmt.Fprintf(&b, "[linters.settings.depguard.rules.%s]\n", r.name)
		keys := []string{"files"}
		if r.strict {
			keys = append(keys, "list-mode", "allow")
		}
		if len(r.deny) > 0 {
			keys = append(keys, "deny")
		}
		width := 0
		for _, key := range keys {
			width = max(width, len(key))
		}
		key := func(name string) string { return name + strings.Repeat(" ", width-len(name)) + " = " }
		writeArray(&b, key("files"), quoteAll(r.files))
		if r.strict {
			b.WriteString(key("list-mode") + `"strict"` + "\n")
			writeArray(&b, key("allow"), quoteAll(r.allow))
		}
		if len(r.deny) > 0 {
			var entries []string
			for _, d := range r.deny {
				entries = append(entries, fmt.Sprintf("{ pkg = %s, desc = %s }", quote(d.pkg), quote(d.desc)))
			}
			writeArray(&b, key("deny"), entries)
		}
	}
	return b.String()
}

func writeArray(b *strings.Builder, prefix string, values []string) {
	b.WriteString(prefix + "[\n")
	for _, value := range values {
		b.WriteString("  " + value + ",\n")
	}
	b.WriteString("]\n")
}

func quoteAll(values []string) []string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = quote(value)
	}
	return quoted
}

func quote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

// splice replaces the generated block of config, markers included.
func splice(config []byte, block string) ([]byte, error) {
	begin := bytes.Index(config, []byte(beginMarker+"\n"))
	end := bytes.Index(config, []byte(endMarker+"\n"))
	if begin < 0 || end < begin {
		return nil, errors.New("the configuration has no generated layer table markers")
	}
	var b bytes.Buffer
	b.Write(config[:begin])
	b.WriteString(beginMarker + "\n\n")
	b.WriteString(block)
	b.WriteString("\n" + endMarker + "\n")
	b.Write(config[end+len(endMarker)+1:])
	return b.Bytes(), nil
}
