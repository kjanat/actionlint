//go:build wasm

package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"syscall/js"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/render"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

// Set by the site build to reject catalogue/WASM deployment mismatches.
var catalogueDigest string

var (
	window      = js.Global().Get("window")
	latestCheck uint64
)

func fail(err error, when string) {
	window.Call("showError", err.Error()+" on "+when)
}

func encodeErrorAsMap(d diagnostic.Diagnostic, src []byte) map[string]any {
	f := render.FindingOf(d, src).TemplateFields()
	obj := make(map[string]any, 5)
	obj["message"] = f.Message
	obj["line"] = f.Line
	obj["column"] = f.Column
	obj["endColumn"] = f.EndColumn
	obj["kind"] = f.Kind
	return obj
}

func lint(source string, check uint64) {
	src := []byte(source)
	result, err := application.Analyze(context.Background(), application.Options{WorkingDir: "/"}, []workspace.Source{{Path: "test.yaml", Content: src}})
	if check != latestCheck {
		return // An edit superseded this worker request.
	}
	if err != nil {
		fail(err, "applying lint rules")
		return
	}

	ret := make([]any, 0, len(result.Diagnostics))
	for _, err := range result.Diagnostics {
		ret = append(ret, encodeErrorAsMap(err, src))
	}

	window.Call("onCheckCompleted", js.ValueOf(ret), source)
}

func runActionlint(_this js.Value, args []js.Value) any {
	source := args[0].String()
	latestCheck++
	go lint(source, latestCheck)
	return nil
}

func main() {
	value := window.Get("actionlintCatalogue")
	if value.Type() != js.TypeString {
		fail(errors.New("action catalogue was not loaded"), "initializing action metadata")
		return
	}
	data := []byte(value.String())
	window.Delete("actionlintCatalogue")
	if catalogueDigest == "" || fmt.Sprintf("%x", sha256.Sum256(data)) != catalogueDigest {
		fail(errors.New("action catalogue does not match this playground build; reload the page"), "initializing action metadata")
		return
	}
	if err := application.LoadActionCatalog(data); err != nil {
		fail(err, "initializing action metadata")
		return
	}
	window.Set("runActionlint", js.FuncOf(runActionlint))
	window.Set("invalidateActionlint", js.FuncOf(func(js.Value, []js.Value) any {
		latestCheck++
		return nil
	}))
	window.Call("dismissLoading")
	latestCheck++
	go lint(window.Call("getYamlSource").String(), latestCheck) // Show the first result
	select {}
}
