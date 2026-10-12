package engine

import (
	"net"
	"strings"
)

// Where is which engine a front end is talking to: its own (embedded) or a
// jitllmd's (connected), and for a service its address and version.
type Where struct {
	// Service is a connected engine; false is the embedded one.
	Service bool
	// Addr and Version are the service's, as it answered the probe.
	Addr    string
	Version string
	// Lost is a service that stopped answering: the app is still connected
	// to it, never silently to an engine of its own.
	Lost bool
}

// Badge is the one line both front ends show for the engine, always:
//
//	Service · 127.0.0.1:8080 (jitllmd 0.9.0)
//	Embedded · API off
//	Embedded · API on :8080
//
// api is the address the embedded engine's API listens on, empty when it is
// off.
func Badge(w Where, api string) string {
	if w.Service {
		// A lost service says so first, where a narrow badge still shows it.
		s := "Service · "
		if w.Lost {
			s += "not answering · "
		}
		s += w.Addr
		if w.Version != "" {
			s += " (jitllmd " + w.Version + ")"
		}
		return s
	}
	if api == "" {
		return "Embedded · API off"
	}
	return "Embedded · API on " + portOf(api)
}

// portOf is ":8080" for a loopback address, since the host says nothing a
// person needs, and the whole address otherwise: an API open to the network
// must say so.
func portOf(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "localhost" || strings.HasPrefix(host, "127.") || host == "::1" {
		return ":" + port
	}
	return addr
}
