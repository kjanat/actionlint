package manifests_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestInlineProgramManifestContract(t *testing.T) {
	t.Parallel()
	document := strings.Replace(minimal, `"entry": "lint",`, `"entry": "lint", "program":{"filename":"actionlint.mjs","inline":"export {};"},`, 1)
	document = strings.Replace(document, `"args": ["--format=json", "{flag:dialect:--shell}", "{user-args}", "-"]`, `"args":["{program-file}"]`, 1)
	manifest, err := manifests.Parse([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Process == nil || manifest.Process.Program == nil || manifest.Process.Program.Filename != "actionlint.mjs" {
		t.Fatal("embedded program lost", manifest.Process)
	}
	for name, invalid := range map[string]string{
		"missing-placeholder":    strings.Replace(document, `["{program-file}"]`, `[]`, 1),
		"duplicate-placeholder":  strings.Replace(document, `["{program-file}"]`, `["{program-file}","{program-file}"]`, 1),
		"partial-placeholder":    strings.Replace(document, `["{program-file}"]`, `["--script={program-file}"]`, 1),
		"missing-program":        strings.Replace(document, `"program":{"filename":"actionlint.mjs","inline":"export {};"},`, "", 1),
		"unsafe-filename":        strings.Replace(document, `"actionlint.mjs"`, `"../actionlint.mjs"`, 1),
		"unknown-program-member": strings.Replace(document, `"inline":"export {};"`, `"inline":"export {};","url":"https://example.com/source"`, 1),
		"empty-source":           strings.Replace(document, `"inline":"export {};"`, `"inline":""`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := manifests.Parse([]byte(invalid)); err == nil {
				t.Fatal("invalid program contract accepted")
			}
		})
	}
}

func TestInlineProgramTypedValidation(t *testing.T) {
	t.Parallel()
	valid := manifests.ProcessSpec{Args: []string{"{program-file}"}, Program: &manifests.InlineProgram{Filename: "actionlint.mjs", Inline: "export {};"}}
	for _, program := range []manifests.InlineProgram{{Filename: "bad/name.mjs", Inline: "x"}, {Filename: "ok.mjs", Inline: ""}, {Filename: "ok.mjs", Inline: strings.Repeat("é", manifests.MaxInlineProgramBytes/2+1)}, {Filename: "ok.mjs", Inline: string([]byte{255})}} {
		invalid := valid
		invalid.Program = &program
		if err := invalid.ValidateProgram(); err == nil {
			t.Fatal("invalid typed program accepted")
		}
	}
	valid.Version = &manifests.VersionProbe{Args: []string{"--version"}, Pattern: "^(.+)$"}
	if err := valid.ValidateProgram(); err != nil {
		t.Fatal("entry version probe rejected", err)
	}
	valid.Version.Args = []string{"{program-file}", "--version"}
	if err := valid.ValidateProgram(); err == nil {
		t.Fatal("version probe silently substituted program")
	}
}
