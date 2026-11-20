// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package commands

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// goaBuilderParams returns the parameter names of the generated function
// named by its fully qualified name (as goaFuncName returns), by locating
// the package's source with `go list` and parsing it.
func goaBuilderParams(t *testing.T, qualifiedName string) []string {
	t.Helper()
	dot := strings.LastIndex(qualifiedName, ".")
	if dot < 0 {
		t.Fatalf("unexpected function name %q", qualifiedName)
	}
	pkgPath, funcName := qualifiedName[:dot], qualifiedName[dot+1:]

	out, err := exec.Command("go", "list", "-f", "{{.Dir}}", pkgPath).Output()
	if err != nil {
		t.Fatalf("go list %s: %v", pkgPath, err)
	}
	dir := strings.TrimSpace(string(out))
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != funcName {
				continue
			}
			var params []string
			for _, field := range fn.Type.Params.List {
				for _, name := range field.Names {
					params = append(params, name.Name)
				}
			}
			return params
		}
	}
	t.Fatalf("%s not found in %s", funcName, dir)
	return nil
}

// checkGoaEndpointSpecs asserts each spec's Args match its builder's
// generated parameter names exactly and in order, and that flag names are
// unique within each command.
func checkGoaEndpointSpecs[C any](t *testing.T, specs []goaEndpointSpec[C]) {
	t.Helper()
	for _, spec := range specs {
		t.Run(spec.Name, func(t *testing.T) {
			want := goaBuilderParams(t, goaFuncName(spec.Builder))
			got := make([]string, len(spec.Args))
			seenFlags := map[string]bool{}
			for i, arg := range spec.Args {
				got[i] = arg.Param
				if arg.Kind == goaArgBearerToken {
					continue
				}
				if arg.Flag == "" {
					t.Errorf("argument %s has no flag", arg.Param)
				}
				if seenFlags[arg.Flag] {
					t.Errorf("flag --%s is mapped more than once", arg.Flag)
				}
				if slices.Contains(goaReservedFlagNames, arg.Flag) {
					t.Errorf("flag --%s for %s is reserved; give the argument a different flag name", arg.Flag, arg.Param)
				}
				seenFlags[arg.Flag] = true
			}
			if !slices.Equal(got, want) {
				t.Errorf("Args do not match %s parameters in order\n got: %v\nwant: %v",
					goaFuncName(spec.Builder), got, want)
			}
		})
	}
}

// TestGoaBuilderSignatures guards every Goa-backed command against
// upstream builder signature changes, including same-typed parameters
// being reordered, which still compiles.
func TestGoaBuilderSignatures(t *testing.T) {
	t.Run("query-svc", func(t *testing.T) {
		checkGoaEndpointSpecs(t, queryServiceEndpoints())
	})
}
