//go:build wasm

package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"syscall/js"

	"actionlint.kjanat.dev"
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

// The JS worker returns a Promise. Run lint in a goroutine so the Go callback
// returns to the browser event loop before waiting for its resolution.
func shellcheck(args []string, stdin string) ([]byte, error) {
	type response struct {
		output []byte
		err    error
	}
	result := make(chan response, 1)
	resolve := js.FuncOf(func(_ js.Value, args []js.Value) any {
		result <- response{output: []byte(args[0].String())}
		return nil
	})
	reject := js.FuncOf(func(_ js.Value, args []js.Value) any {
		result <- response{err: fmt.Errorf("ShellCheck worker: %s", js.Global().Get("String").Invoke(args[0]).String())}
		return nil
	})
	defer resolve.Release()
	defer reject.Release()
	arguments := make([]any, len(args))
	for i, arg := range args {
		arguments[i] = arg
	}
	window.Call("runShellcheck", js.ValueOf(arguments), stdin).Call("then", resolve, reject)
	r := <-result
	return r.output, r.err
}

func lint(source string, check uint64) {
	opts := actionlint.LinterOptions{WorkingDir: "/", OnRulesCreated: func(rules []actionlint.Rule) []actionlint.Rule {
		return append(rules, actionlint.NewRuleShellcheckWithRunner(shellcheck))
	}}
	linter, err := actionlint.NewLinter(io.Discard, &opts)
	if err != nil {
		fail(err, "creating linter instance")
		return
	}

	src := []byte(source)
	errs, err := linter.Lint("test.yaml", src, nil)
	if check != latestCheck {
		return // An edit superseded this worker request.
	}
	if err != nil {
		fail(err, "applying lint rules")
		return
	}

	ret := make([]any, 0, len(errs))
	for _, err := range errs {
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
	if err := actionlint.LoadPopularActions(data); err != nil {
		fail(err, "initializing action metadata")
		return
	}
	if window.Get("runShellcheck").Type() != js.TypeFunction {
		fail(errors.New("ShellCheck worker was not loaded"), "initializing ShellCheck")
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
