package app

import (
	"github.com/gogpu/ui/state"

	"github.com/jitllm/jitllm/common/api"
	core "github.com/jitllm/jitllm/common/engine"
	"github.com/jitllm/jitllm/internal/localserver"
	"github.com/jitllm/jitllm/server"
)

// ServeAPI serves e, the window's own engine, as jitllm's API while the API
// setting is on. The address is read when the setting turns on, not on every
// keystroke in its field; turning the API off and on again applies a new one.
// The API's model folder follows the first of the window's.
//
// Where a jitllmd already answers at the address the API is refused, the
// setting goes back off and the window offers to connect to the service
// instead (OfferService): a second engine is never started silently.
//
// Call it before the engine's own OnShutdown, so the API stops taking requests
// before the engine closes. It returns what stops it, for an engine switch.
func (s *Shell) ServeAPI(e *server.Engine) (stop func()) {
	dir := func() {
		if dirs := s.Store.ModelDirs.Get(); len(dirs) > 0 {
			e.SetModelDir(dirs[0])
		}
	}
	dir()
	stopped := false
	on(s.Store.ModelDirs, func() {
		if !stopped {
			dir()
		}
	})

	var srv *api.Server
	sync := func() {
		if stopped {
			return
		}
		var msg string
		var held localserver.Info
		srv, msg, held = api.Toggle(srv, s.Store.API.Get(), s.Store.APIAddr.Get(), e)
		serving := ""
		if srv != nil {
			serving = srv.Addr()
		}
		s.Store.APIServing.Set(serving)
		if msg != "" {
			s.SetStatus(msg)
		}
		if held.Addr != "" {
			s.Store.API.Set(false)
			s.OfferService(held, "Turning the API on here would start a second engine beside it, sharing this computer's GPU memory.")
		}
	}
	on(s.Store.API, sync)
	if s.Store.API.Get() {
		sync()
	}
	stop = func() {
		stopped = true
		if srv != nil {
			srv.Close()
			srv = nil
		}
		s.Store.APIServing.Set("")
	}
	s.OnShutdown(stop)
	return stop
}

// OfferService says a jitllmd is running at in.Addr, why that matters (why),
// and offers to connect to it when the window can (ConnectTo is set).
func (s *Shell) OfferService(in localserver.Info, why string) {
	name := "jitllmd"
	if in.Version != "" {
		name += " " + in.Version
	}
	title := name + " is running at " + in.Addr
	if s.ConnectTo == nil {
		s.Alert(title, why)
		return
	}
	addr := in.Addr
	s.Confirm(title, why+" Connect to it instead?", "Connect", func() { s.ConnectTo(addr) })
}

// Badge is the engine line the window always shows (core.Badge): which
// engine, and the API's state or the service's address and version.
func (s *Shell) Badge() state.ReadonlySignal[string] {
	st := s.Store
	return state.NewComputed(func() string {
		return core.Badge(st.Engine.Get(), st.APIServing.Get())
	}, st.Engine.AsReadonly(), st.APIServing.AsReadonly())
}
