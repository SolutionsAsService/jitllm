package install

import (
	"os"
	"strconv"

	"github.com/jitllm/jitllm/internal/localserver"
	"github.com/jitllm/jitllm/internal/localserver/service"
)

// AgentLabel names the launchd user agent that starts jitllmd at login. It
// is the app's bundle identifier (ui/cmd/pack) with the program's name.
const AgentLabel = service.Label

// AgentAddr is where the agent's jitllmd listens: loopback only, as the app's
// own API defaults to, and on the same port, so the two are not both on --
// the settings screen says so beside the switch.
const AgentAddr = localserver.DefaultAddr

// Agent is the launchd user agent for jitllmd, rooted at a home directory.
// It is `jitllmd service` on macOS (internal/localserver/service): the app's
// switch and the command write the same agent.
type Agent struct {
	Home string
}

func (a Agent) svc() service.Service {
	return service.Service{GOOS: "darwin", Home: a.Home, UID: os.Getuid()}
}

// Plist is where the agent's property list is written.
func (a Agent) Plist() string { return a.svc().File() }

// Log is where jitllmd's output goes while it runs under launchd.
func (a Agent) Log() string { return a.svc().Log() }

// Installed reports whether the agent's property list is in place.
func (a Agent) Installed() bool { return a.svc().Installed() }

// PlistFor is the property list that runs jitllmd serve on AgentAddr over
// the models directory.
func (a Agent) PlistFor(jitllmd, models string) string {
	return a.svc().Render(service.Args(jitllmd, models, AgentAddr))
}

// Install writes the agent and starts it in the person's GUI session. An
// agent already there is replaced.
func (a Agent) Install(jitllmd, models string) error {
	return a.svc().Install(service.Args(jitllmd, models, AgentAddr))
}

// Remove stops the agent and deletes its property list.
func (a Agent) Remove() error { return a.svc().Uninstall() }

// domain is launchd's domain for the person's GUI session.
func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }
