package driver

import (
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/check"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/syntax"
)

// topLevelDefInfo collects the static def info for the top-level defs in
// stmts. Test-only: the checker tests use it to build a visible-def map
// directly, without the import-expansion path that populates it in
// production via recordTopLevelDefInfo.
func topLevelDefInfo(stmts []syntax.Stmt) map[string]check.DefStaticInfo {
	out := make(map[string]check.DefStaticInfo)
	recordTopLevelDefInfo(out, stmts)
	return out
}
