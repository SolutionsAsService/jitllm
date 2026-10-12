package engine

import (
	"context"
	"errors"

	"github.com/jitllm/jitllm/internal/localserver"
)

// Beside asks whether a jitllmd answers at addr (the default address when
// empty) while this front end runs an embedded engine, and returns it with
// the warning to show. Ask it before the front end's own API starts, or the
// probe finds the app itself.
func Beside(ctx context.Context, addr string) (in localserver.Info, warning string, ok bool) {
	if addr == "" {
		addr = localserver.DefaultAddr
	}
	// An incompatible jitllmd is still a second engine on the GPU.
	in, err := localserver.Probe(ctx, addr)
	if err != nil && !errors.Is(err, localserver.ErrIncompatible) {
		return in, "", false
	}
	return in, SharesMemory(in), true
}

// SharesMemory is the warning for an embedded engine started beside the
// service in.
func SharesMemory(in localserver.Info) string {
	name := "jitllmd"
	if in.Version != "" {
		name += " " + in.Version
	}
	return name + " is running at " + in.Addr + " beside this app's own engine: the two share this computer's GPU memory and RAM, and each loads its own copy of a model."
}
