package architecture_test

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	"golang.org/x/tools/go/packages"
)

const executionPackageMode = packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo

func TestAnalyzerInvocationsUseSharedExecutor(t *testing.T) {
	loaded, err := packages.Load(&packages.Config{
		Context: t.Context(), Dir: "../..",
		Mode: executionPackageMode,
	}, "./...")
	if err != nil {
		t.Fatal(err)
	}
	contract := executionContract(t, loaded)
	owner := runtime.FuncForPC(reflect.ValueOf(analyzerhost.RunOne).Pointer()).Name()
	calls, violations := analyzerExecutionViolations(loaded, contract, owner)
	for _, violation := range violations {
		t.Error(violation)
	}
	if calls != 1 {
		t.Errorf("expected one analyzer invocation in the shared executor, found %d", calls)
	}
	// Check production builds on the supported desktop and browser targets.
	// Tests, reflection and unsafe/linkname calls are outside this typed check.
	for _, target := range []struct{ goos, goarch string }{
		{"linux", "amd64"}, {"windows", "amd64"}, {"darwin", "arm64"}, {"js", "wasm"},
	} {
		if target.goos == runtime.GOOS && target.goarch == runtime.GOARCH {
			continue
		}
		t.Run(target.goos+"/"+target.goarch, func(t *testing.T) {
			cross, err := packages.Load(&packages.Config{
				Context: t.Context(), Dir: "../..", Mode: executionPackageMode, Env: executionEnvironment(target.goos, target.goarch),
			}, "./...")
			if err != nil {
				t.Fatal(err)
			}
			count, violations := analyzerExecutionViolations(cross, executionContract(t, cross), owner)
			for _, violation := range violations {
				t.Error(violation)
			}
			if count != 1 {
				t.Errorf("expected one analyzer invocation in the shared executor, found %d", count)
			}
		})
	}
}

func executionEnvironment(goos, goarch string) []string {
	var environment []string
	for _, variable := range os.Environ() {
		key, _, _ := strings.Cut(variable, "=")
		if !strings.EqualFold(key, "GOOS") && !strings.EqualFold(key, "GOARCH") {
			environment = append(environment, variable)
		}
	}
	return append(environment, "GOOS="+goos, "GOARCH="+goarch)
}

func TestAnalyzerExecutorRejectsBypasses(t *testing.T) {
	owner := runtime.FuncForPC(reflect.ValueOf(analyzerhost.RunOne).Pointer()).Name()
	ownerPackage := owner[:strings.LastIndexByte(owner, '.')]
	contractType := reflect.TypeFor[analyzer.Analyzer]()
	loaded, err := packages.Load(&packages.Config{
		Context: t.Context(), Dir: "../..", Mode: executionPackageMode,
	}, ownerPackage, contractType.PkgPath())
	if err != nil {
		t.Fatal(err)
	}
	executionContract(t, loaded)
	t.Run("method alias cannot bypass executor", func(t *testing.T) {
		var ownerData, contractData *packages.Package
		for _, pkg := range loaded {
			if pkg.PkgPath == ownerPackage {
				ownerData = pkg
			}
			if pkg.PkgPath == contractType.PkgPath() {
				contractData = pkg
			}
		}
		if ownerData == nil || len(ownerData.GoFiles) == 0 || contractData == nil || len(contractData.GoFiles) == 0 {
			t.Fatal("executor source package unavailable")
		}
		probe := filepath.Join(filepath.Dir(ownerData.GoFiles[0]), "executor_probe.go")
		content := fmt.Sprintf(`package %s
import (
 "context"
 renamed %q
 "actionlint.kjanat.dev/v2/diagnostic"
)
type analyzerExecutionProbe = renamed.%s
var escapedAnalyzerProbe = analyzerExecutionProbe.Analyze
type analyzerExecutionOnly interface {
 Analyze(context.Context, renamed.Input) ([]diagnostic.Diagnostic, error)
}
func narrowAnalyzerProbe(a renamed.Analyzer, ctx context.Context, in renamed.Input) {
 var narrow analyzerExecutionOnly = a
 narrow.Analyze(ctx, in)
}
func methodValueProbe(a renamed.Analyzer) { _ = a.Analyze }
type promotedAnalyzerProbe struct { renamed.Analyzer }
var promotedAnalyzerMethod = promotedAnalyzerProbe.Analyze
func genericAnalyzerProbe[T analyzerExecutionOnly](a T, ctx context.Context, in renamed.Input) {
 a.Analyze(ctx, in)
}
`, ownerData.Name, contractType.PkgPath(), contractType.Name())
		contractProbe := filepath.Join(filepath.Dir(contractData.GoFiles[0]), "executor_contract_probe.go")
		contractContent := fmt.Sprintf(`package %s
import "context"
func contractPackageExecutorProbe(a Analyzer, ctx context.Context, in Input) { a.Analyze(ctx, in) }
`, contractData.Name)
		withProbe, err := packages.Load(&packages.Config{
			Context: t.Context(), Dir: "../..", Mode: executionPackageMode,
			Overlay: map[string][]byte{probe: []byte(content), contractProbe: []byte(contractContent)},
		}, ownerPackage, contractType.PkgPath())
		if err != nil {
			t.Fatal(err)
		}
		calls, violations := analyzerExecutionViolations(withProbe, executionContract(t, withProbe), owner)
		if calls != 7 || len(violations) != 6 {
			t.Fatalf("aliased method escape not detected: calls=%d violations=%v", calls, violations)
		}
		for _, violation := range violations {
			if !strings.Contains(violation, "executor_probe.go") && !strings.Contains(violation, "executor_contract_probe.go") {
				t.Errorf("unexpected violation outside probe: %s", violation)
			}
		}
	})
	t.Run("second invocation inside owner is counted", func(t *testing.T) {
		for _, pkg := range loaded {
			if pkg.PkgPath != ownerPackage {
				continue
			}
			for _, file := range pkg.Syntax {
				for _, declaration := range file.Decls {
					function, ok := declaration.(*ast.FuncDecl)
					if !ok || function.Name.Name != owner[len(ownerPackage)+1:] {
						continue
					}
					var statement *ast.AssignStmt
					var invocation *ast.CallExpr
					ast.Inspect(function.Body, func(node ast.Node) bool {
						assignment, ok := node.(*ast.AssignStmt)
						if !ok || len(assignment.Rhs) != 1 {
							return true
						}
						call, ok := assignment.Rhs[0].(*ast.CallExpr)
						if !ok {
							return true
						}
						selector, ok := call.Fun.(*ast.SelectorExpr)
						if ok && selector.Sel.Name == "Analyze" {
							statement, invocation = assignment, call
						}
						return true
					})
					if statement == nil {
						t.Fatal("cannot locate executor invocation for duplicate-call probe")
					}
					position := pkg.Fset.Position(statement.Pos())
					content, err := os.ReadFile(position.Filename)
					if err != nil {
						t.Fatal(err)
					}
					callStart, callEnd := pkg.Fset.Position(invocation.Pos()).Offset, pkg.Fset.Position(invocation.End()).Offset
					duplicate := string(content[:position.Offset]) + string(content[callStart:callEnd]) + "\n" + string(content[position.Offset:])
					withProbe, err := packages.Load(&packages.Config{
						Context: t.Context(), Dir: "../..", Mode: executionPackageMode,
						Overlay: map[string][]byte{position.Filename: []byte(duplicate)},
					}, ownerPackage, contractType.PkgPath())
					if err != nil {
						t.Fatal(err)
					}
					calls, violations := analyzerExecutionViolations(withProbe, executionContract(t, withProbe), owner)
					if calls != 2 || len(violations) != 0 {
						t.Fatalf("duplicate invocation was not counted: calls=%d violations=%v", calls, violations)
					}
					return
				}
			}
		}
		t.Fatal("executor declaration not found")
	})
	t.Run("build constraint exposes bypass only on target", func(t *testing.T) {
		var ownerData *packages.Package
		for _, pkg := range loaded {
			if pkg.PkgPath == ownerPackage {
				ownerData = pkg
			}
		}
		if ownerData == nil || len(ownerData.GoFiles) == 0 {
			t.Fatal("executor source package unavailable")
		}
		targetOS, targetArch := "js", "wasm"
		if runtime.GOOS == targetOS && runtime.GOARCH == targetArch {
			targetOS, targetArch = "linux", "amd64"
		}
		probe := filepath.Join(filepath.Dir(ownerData.GoFiles[0]), "executor_platform_probe.go")
		content := fmt.Sprintf(`//go:build %s && %s

package %s
import (
 "context"
 renamed %q
)
func platformExecutorProbe(a renamed.Analyzer, ctx context.Context, in renamed.Input) { a.Analyze(ctx, in) }
`, targetOS, targetArch, ownerData.Name, contractType.PkgPath())
		for _, target := range []struct {
			goos, goarch      string
			calls, violations int
		}{
			{runtime.GOOS, runtime.GOARCH, 1, 0},
			{targetOS, targetArch, 2, 1},
		} {
			withProbe, err := packages.Load(&packages.Config{
				Context: t.Context(), Dir: "../..", Mode: executionPackageMode,
				Env: executionEnvironment(target.goos, target.goarch), Overlay: map[string][]byte{probe: []byte(content)},
			}, ownerPackage, contractType.PkgPath())
			if err != nil {
				t.Fatal(err)
			}
			calls, violations := analyzerExecutionViolations(withProbe, executionContract(t, withProbe), owner)
			if calls != target.calls || len(violations) != target.violations {
				t.Fatalf("%s/%s build constraint not enforced: calls=%d violations=%v", target.goos, target.goarch, calls, violations)
			}
			if len(violations) == 1 && !strings.Contains(violations[0], "executor_platform_probe.go") {
				t.Fatalf("target bypass missing from violations: %v", violations)
			}
		}
	})
}

func executionContract(t *testing.T, loaded []*packages.Package) *types.Signature {
	t.Helper()
	contractType := reflect.TypeFor[analyzer.Analyzer]()
	var contract *types.Interface
	for _, pkg := range loaded {
		for _, err := range pkg.Errors {
			t.Error(err)
		}
		if pkg.PkgPath == contractType.PkgPath() {
			object := pkg.Types.Scope().Lookup(contractType.Name())
			if object == nil {
				t.Fatal("analyzer contract missing from type information")
			}
			contract, _ = object.Type().Underlying().(*types.Interface)
		}
	}
	if t.Failed() || contract == nil {
		t.Fatal("cannot verify analyzer execution without complete type information")
	}
	for method := range contract.Methods() {
		if method.Name() == "Analyze" {
			signature, ok := method.Type().(*types.Signature)
			if !ok {
				t.Fatal("analyzer method has no signature")
			}
			return signature
		}
	}
	t.Fatal("analyzer contract has no Analyze method")
	return nil
}

func analyzerExecutionViolations(loaded []*packages.Package, contract *types.Signature, owner string) (int, []string) {
	separator := strings.LastIndexByte(owner, '.')
	ownerPackage, ownerFunction := owner[:separator], owner[separator+1:]
	calls := 0
	var violations []string
	for _, pkg := range loaded {
		for selector, selection := range pkg.TypesInfo.Selections {
			if selection.Obj().Name() != "Analyze" {
				continue
			}
			if !types.Identical(selection.Obj().Type(), contract) {
				continue
			}
			calls++
			allowed := false
			if pkg.PkgPath == ownerPackage {
				for _, file := range pkg.Syntax {
					for _, declaration := range file.Decls {
						function, ok := declaration.(*ast.FuncDecl)
						if !ok || function.Recv != nil || function.Name.Name != ownerFunction {
							continue
						}
						ast.Inspect(function, func(node ast.Node) bool {
							if call, ok := node.(*ast.CallExpr); ok {
								callee := call.Fun
								for {
									parentheses, ok := callee.(*ast.ParenExpr)
									if !ok {
										break
									}
									callee = parentheses.X
								}
								allowed = allowed || callee == selector
							}
							return true
						})
					}
				}
			}
			if !allowed {
				violations = append(violations, fmt.Sprintf("%s: analyzer invocation or escaped method bypasses %s", pkg.Fset.Position(selector.Pos()), owner))
			}
		}
	}
	return calls, violations
}
