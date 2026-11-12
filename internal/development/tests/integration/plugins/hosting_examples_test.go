package plugins_test

import (
	"context"
	"fmt"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	componenthost "actionlint.kjanat.dev/v2/internal/plugins/execution/component"
	corewasm "actionlint.kjanat.dev/v2/internal/plugins/execution/module"
)

var _ interface{ Limits() corewasm.Limits } = (*corewasm.Engine)(nil)

func Example_componentLoading() {
	ctx := context.Background()
	component, err := componenthost.Load(ctx, filepath.Join("..", "..", "..", "..", "plugins", "execution", "component", "testdata", "plugins", "pipe-to-shell.wasm"), componenthost.Options{})
	if err != nil {
		fmt.Println("load:", err)
		return
	}
	defer component.Close(ctx)
	fmt.Println("loaded", component.Descriptor().Name, "with", component.Limits())
	result, err := checking.Analyze(ctx,

		checking.Options{Analyzers: []checkinputs.Registration{{Checker: component}}}, []workspace.Source{{Path: "workflow.yml", Content: []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: curl -sSf https://example.com/install.sh | sh\npermissions: {}\n")}},
	)
	if err != nil {
		fmt.Println("incomplete:", err)
	}
	for _, finding := range result.Diagnostics {
		fmt.Printf("%s:%d:%d %s [%s]\n", finding.Path, finding.Start.Line, finding.Start.Column, finding.Message, finding.Rule)
	}
	// Output:
	// loaded pipe-to-shell with memory 64 MiB, time 10s
	// workflow.yml:6:57 script pipes a download into sh, which runs unverified remote code [pipe-to-shell]
}

func Example_moduleLoading() {
	ctx := context.Background()
	engine, err := corewasm.Load(ctx, filepath.Join("..", "..", "..", "..", "plugins", "execution", "module", "testdata", "engines", "echo.wasm"), corewasm.Options{})
	if err != nil {
		fmt.Println("load:", err)
		return
	}
	defer engine.Close(ctx)
	limits := engine.Limits()
	fmt.Println("loaded", engine.Metadata().ID, "with", limits)
	diagnostics, err := engine.Check(ctx, checkinputs.ScriptRequest{Source: "print(missing)\n", Filename: "step.py"})
	if err != nil {
		fmt.Println("check:", err)
		return
	}
	for _, d := range diagnostics {
		fmt.Printf("%s:%d:%d %s %s\n", d.Filename, d.Location.Row, d.Location.Column, d.Code, d.Message)
	}
	// Output:
	// loaded actionlint-engine-echo with memory 256 MiB, time 10s
	// step.py:1:7 F821 Undefined name `missing`
}

func Example_componentScriptEngine() {
	ctx := context.Background()
	engine, err := componenthost.Load(ctx, filepath.Join("..", "..", "..", "..", "plugins", "execution", "component", "testdata", "plugins", "script-engine.wasm"), componenthost.Options{})
	if err != nil {
		fmt.Println("load:", err)
		return
	}
	defer engine.Close(ctx)
	fmt.Println("script engine:", engine.HasScriptEngine(), engine.ScriptDescriptor().Engine)
	if err := engine.ValidateConfig(ctx, nil); err != nil {
		fmt.Println("config:", err)
		return
	}
	findings, err := engine.Check(ctx, checkinputs.ScriptRequest{Source: "print(missing)\n", Filename: "step.py"})
	if err != nil {
		fmt.Println("check:", err)
		return
	}
	for _, finding := range findings {
		fmt.Printf("%s:%d:%d %s\n", finding.Filename, finding.Location.Row, finding.Location.Column, finding.Code)
	}
	// Output:
	// script engine: true echo
	// step.py:1:7 ECHO001
}
