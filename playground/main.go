//go:build wasm

package main

import (
	"io"
	"syscall/js"

	"github.com/kjanat/actionlint"
)

var (
	window = js.Global().Get("window")
)

func fail(err error, when string) {
	window.Call("showError", err.Error()+" on "+when)
}

func encodeErrorAsMap(err *actionlint.Error, src []byte) map[string]any {
	f := err.GetTemplateFields(src)
	obj := make(map[string]any, 5)
	obj["message"] = f.Message
	obj["line"] = f.Line
	obj["column"] = f.Column
	obj["endColumn"] = f.EndColumn
	obj["kind"] = f.Kind
	return obj
}

func lint(source string) any {
	opts := actionlint.LinterOptions{}
	linter, err := actionlint.NewLinter(io.Discard, &opts)
	if err != nil {
		fail(err, "creating linter instance")
		return nil
	}

	src := []byte(source)
	errs, err := linter.Lint("test.yaml", src, nil)
	if err != nil {
		fail(err, "applying lint rules")
		return nil
	}

	ret := make([]any, 0, len(errs))
	for _, err := range errs {
		ret = append(ret, encodeErrorAsMap(err, src))
	}

	window.Call("onCheckCompleted", js.ValueOf(ret))

	return nil
}

func runActionlint(_this js.Value, args []js.Value) any {
	source := args[0].String()
	return lint(source)
}

func main() {
	window.Set("runActionlint", js.FuncOf(runActionlint))
	window.Call("dismissLoading")
	lint(window.Call("getYamlSource").String()) // Show the first result
	select {}
}
