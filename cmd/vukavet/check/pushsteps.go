package check

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// PushSteps reports a step a chain runs straight after a Push that reads
// as if it waited for the reply (W0116): Hide, Navigate, Patch, Reload, or a toast
// shown with Then. The steps after a Push run at once, while the push is
// on its way; .Success(…) runs them once it has succeeded. A Remove there
// isn't the push's prediction either, so a failed push doesn't put it back:
// .Before(…) makes it one.
var PushSteps = &analysis.Analyzer{
	Name:     "pushsteps",
	Doc:      "report steps written straight after a Push that run at once, not after its reply",
	URL:      docsURL,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runPushSteps,
}

// pushModifiers are the steps that change the Push before them rather
// than follow it.
var pushModifiers = map[string]bool{"Into": true, "Timeout": true, "Before": true, "Success": true, "Error": true, "After": true, "OnSuccess": true, "OnError": true}

func runPushSteps(pass *analysis.Pass) (any, error) {
	if imported(pass.Pkg, uiPath) == nil {
		return nil, nil
	}
	ins := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	ins.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		step, prev := chainStep(pass, call)
		what := ""
		switch step {
		case "Hide", "Navigate", "Patch", "PatchReplace", "Reload", "Remove":
			what = step
		case "Then":
			if len(call.Args) == 1 && toast(pass, call.Args[0]) {
				what = "the toast"
			}
		}
		if what == "" || !afterPush(pass, prev) {
			return
		}
		args := "…"
		if step == "Reload" {
			args = ""
		}
		help := "move it into the push's Success, .Success(ui.JS." + step + "(" + args + ")), to run it once the push has succeeded"
		if step == "Then" {
			help = "move it into the push's Success, .Success(ui.ShowToast(…)), to show it once the push has succeeded"
		}
		if step == "Remove" {
			report(pass, call, "W0116", "move it into the push's Before, .Before(ui.JS.Remove(…)), to have it put back when the push fails", "Remove after a Push isn't its prediction: it runs at once, while the push is on its way, and stays when the push fails")
			return
		}
		report(pass, call, "W0116", help, "%s runs at once, while the Push before it is on its way, not after its reply", what)
	})
	return nil, nil
}

// chainStep is the method of the ui chain call calls, and the chain it is
// called on.
func chainStep(pass *analysis.Pass, call *ast.CallExpr) (string, ast.Expr) {
	if m, x := method(pass.TypesInfo, call, uiPath, "Commands"); m != "" {
		return m, x
	}
	return method(pass.TypesInfo, call, uiPath, "CommandsOn")
}

// afterPush reports whether x, a chain, ends in a Push, past the steps
// that change it.
func afterPush(pass *analysis.Pass, x ast.Expr) bool {
	for {
		call, ok := ast.Unparen(x).(*ast.CallExpr)
		if !ok {
			return false
		}
		step, prev := chainStep(pass, call)
		switch {
		case step == "Push":
			return true
		case pushModifiers[step]:
			x = prev
		default:
			return false
		}
	}
}

// toast reports whether x is a call of ui's ShowToast.
func toast(pass *analysis.Pass, x ast.Expr) bool {
	call, ok := ast.Unparen(x).(*ast.CallExpr)
	return ok && funcOf(pass.TypesInfo, call, uiPath) == "ShowToast"
}
