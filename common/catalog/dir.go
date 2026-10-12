package catalog

import "github.com/jitllm/jitllm/internal/localserver"

// DefaultDir is where a first run looks for models and puts downloads: models
// ($JITLLM_MODELS) when set, and otherwise the models folder in this system's
// place for application data -- the same folder jitllmd and the CLI use
// (localserver.ModelsDir). dataHome is $XDG_DATA_HOME; the library reads no
// environment, so the command hands both over.
func DefaultDir(models, dataHome string) string {
	return localserver.ModelsDir(models, dataHome)
}
