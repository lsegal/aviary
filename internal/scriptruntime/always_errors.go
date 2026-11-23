package scriptruntime

import (
	"strings"

	"github.com/yuin/gopher-lua/ast"
	"github.com/yuin/gopher-lua/parse"
)

// AlwaysErrors reports whether script can only fail: before doing anything
// else, it calls error(...). Constant locals, such as the message the call
// raises, may come first. A compiler that cannot do a task with the tools it
// has writes such a stub, which must never be promoted.
func AlwaysErrors(script string) bool {
	chunk, err := parse.Parse(strings.NewReader(script), "<script>")
	if err != nil {
		return false
	}
	for _, stmt := range chunk {
		switch s := stmt.(type) {
		case *ast.LocalAssignStmt:
			for _, expr := range s.Exprs {
				if !isConstantExpr(expr) {
					return false
				}
			}
		case *ast.FuncCallStmt:
			return isErrorCall(s.Expr)
		case *ast.ReturnStmt:
			return len(s.Exprs) == 1 && isErrorCall(s.Exprs[0])
		default:
			return false
		}
	}
	return false
}

func isErrorCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.FuncCallExpr)
	if !ok || call.Receiver != nil {
		return false
	}
	ident, ok := call.Func.(*ast.IdentExpr)
	return ok && ident.Value == "error"
}

func isConstantExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case ast.ConstExpr:
		return true
	case *ast.StringConcatOpExpr:
		return isConstantExpr(e.Lhs) && isConstantExpr(e.Rhs)
	default:
		return false
	}
}
