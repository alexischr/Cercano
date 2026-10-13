package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// This guards the actual bootstrap ownership wiring without starting a real
// agent (whose startup can open credentials, conversations and orphan cleanup).
// Runtime observer behavior is tested independently with owned server fixtures.
func TestTUIObserverLivesUntilReturnedAgentCleanup(t *testing.T) {
	file, e := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if e != nil {
		t.Fatal(e)
	}
	var start *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "startGRPCServer" {
			start = fn
		}
	}
	if start == nil {
		t.Fatal("missing startup function")
	}
	isStop := func(call *ast.CallExpr) bool {
		id, ok := call.Fun.(*ast.Ident)
		return ok && id.Name == "stopTUINotices"
	}
	ast.Inspect(start.Body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if d, ok := n.(*ast.DeferStmt); ok && isStop(d.Call) {
			t.Error("observer canceled at startup return instead of returned cleanup")
		}
		return true
	})
	cleanupStops := false
	for _, stmt := range start.Body.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok {
			continue
		}
		for i, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != "cleanup" || i >= len(assign.Rhs) {
				continue
			}
			fn, ok := assign.Rhs[i].(*ast.FuncLit)
			if !ok {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && isStop(call) {
					cleanupStops = true
				}
				return true
			})
		}
	}
	if !cleanupStops {
		t.Fatal("returned cleanup does not cancel observer")
	}
}
