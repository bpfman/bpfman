package runtime

import (
	"fmt"
	"strings"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/ir"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/source"
)

func traceRendered(env *Env, pos source.Pos, rendered string) {
	if env == nil || env.Trace == nil {
		return
	}
	env.Trace(pos, rendered)
}

func traceValue(env *Env, sp source.Span, prefix string, v Value) {
	rendered, err := RenderCompact(v)
	if err != nil {
		rendered = fmt.Sprintf("<unrenderable %s>", v.Kind())
	}

	traceRendered(env, sp.Pos, prefix+rendered)
}

func traceNote(env *Env, sp source.Span, rendered string) {
	traceRendered(env, sp.Pos, rendered)
}

func defTraceText(name string, params []ir.Param) string {
	var b strings.Builder
	b.WriteString("def ")
	b.WriteString(name)
	b.WriteByte('(')
	b.WriteString(ir.ParamList(params))
	b.WriteByte(')')
	return b.String()
}
