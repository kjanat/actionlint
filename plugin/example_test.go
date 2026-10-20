package plugin_test

import (
	"context"
	"fmt"
	"path/filepath"

	"actionlint.kjanat.dev/analysis"
	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/plugin"
)

var _ interface{ Limits() plugin.EngineLimits } = (*plugin.Engine)(nil)

func ExampleLoad() {
	ctx := context.Background()
	component, err := plugin.Load(ctx, filepath.Join("..", "internal", "plugins", "component", "testdata", "plugins", "pipe-to-shell.wasm"), plugin.Options{})
	if err != nil {
		fmt.Println("load:", err)
		return
	}
	defer component.Close(ctx)
	fmt.Println("loaded", component.Descriptor().Name, "with", component.Limits())
	result, err := analysis.Analyze(ctx, analysis.Request{
		Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: curl -sSf https://example.com/install.sh | sh\n")}},
		Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: component}}},
	})
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

func ExampleLoadEngine() {
	ctx := context.Background()
	engine, err := plugin.LoadEngine(ctx, filepath.Join("..", "internal", "plugins", "core", "testdata", "engines", "echo.wasm"), plugin.EngineOptions{})
	if err != nil {
		fmt.Println("load:", err)
		return
	}
	defer engine.Close(ctx)
	limits := engine.Limits()
	fmt.Println("loaded", engine.Metadata().ID, "with", limits)
	diagnostics, err := engine.Check(ctx, analyzer.ScriptRequest{Source: "print(missing)\n", Filename: "step.py"})
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

func ExampleLoad_scriptEngine() {
	ctx := context.Background()
	engine, err := plugin.Load(ctx, filepath.Join("..", "internal", "plugins", "component", "testdata", "plugins", "script-engine.wasm"), plugin.Options{})
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
	findings, err := engine.Check(ctx, analyzer.ScriptRequest{Source: "print(missing)\n", Filename: "step.py"})
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
