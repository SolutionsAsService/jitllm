package localserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// StateFile is the models folder's settings file: per container (by file
// name), its aliases and whether it is disabled. jitllmd reads and writes it
// (SetModelOptions), `jitllm run` reads it to find a model by alias when no
// service answers, and `jitllm convert NAME` adds the catalog name as an
// alias. It lives in the folder, so it moves with the models.
const StateFile = "jitllm-models.json"

// ModelState is one container's settings.
type ModelState struct {
	Aliases  []string `json:"aliases,omitempty"`
	Disabled bool     `json:"disabled,omitempty"`
}

// State is the whole file.
type State struct {
	Models map[string]*ModelState `json:"models"`
}

// ReadState reads dir's state file; a folder without one has no settings.
func ReadState(dir string) (State, error) {
	st := State{Models: map[string]*ModelState{}}
	b, err := os.ReadFile(filepath.Join(dir, StateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, fmt.Errorf("%s: %w", filepath.Join(dir, StateFile), err)
	}
	if st.Models == nil {
		st.Models = map[string]*ModelState{}
	}
	return st, nil
}

// WriteState replaces dir's state file whole (written beside, renamed), so a
// crash leaves the old settings or the new ones. Entries with nothing set
// are dropped.
func WriteState(dir string, st State) error {
	for k, m := range st.Models {
		if len(m.Aliases) == 0 && !m.Disabled {
			delete(st.Models, k)
		}
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(dir, StateFile)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Stem is a file name without its extension.
func Stem(file string) string { return strings.TrimSuffix(file, filepath.Ext(file)) }

// Names reports whether name names the container file (a base name) with
// these aliases: its file name, its stem or an alias.
func Names(file string, aliases []string, name string) bool {
	return name == file || name == Stem(file) || slices.Contains(aliases, name)
}

// ValidAlias is nil for a name usable as an alias: not empty, no slash, no
// surrounding space.
func ValidAlias(a string) error {
	if a == "" || strings.ContainsAny(a, `/\`) || strings.TrimSpace(a) != a {
		return fmt.Errorf("alias %q: an alias is a non-empty name with no slash or surrounding space", a)
	}
	return nil
}

// Find looks name up among the .jlm containers of dirs, in order, by file
// name, stem or alias. It returns the path and whether that container is
// disabled; "" when no container answers to the name.
func Find(dirs []string, name string) (path string, disabled bool, err error) {
	for _, d := range dirs {
		ents, err := os.ReadDir(d)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", false, err
		}
		st, err := ReadState(d)
		if err != nil {
			return "", false, err
		}
		for _, e := range ents {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".jlm") {
				continue
			}
			m := st.Models[e.Name()]
			if m == nil {
				m = &ModelState{}
			}
			if Names(e.Name(), m.Aliases, name) {
				return filepath.Join(d, e.Name()), m.Disabled, nil
			}
		}
	}
	return "", false, nil
}

// AddAlias gives the container file (a base name in dir) the alias, unless
// another container of dir already answers to it. It reports whether the
// alias was added.
func AddAlias(dir, file, alias string) (bool, error) {
	if err := ValidAlias(alias); err != nil {
		return false, err
	}
	if alias == file || alias == Stem(file) {
		return false, nil
	}
	if p, _, err := Find([]string{dir}, alias); err != nil || (p != "" && filepath.Base(p) != file) {
		return false, err
	}
	st, err := ReadState(dir)
	if err != nil {
		return false, err
	}
	m := st.Models[file]
	if m == nil {
		m = &ModelState{}
		st.Models[file] = m
	}
	if slices.Contains(m.Aliases, alias) {
		return false, nil
	}
	m.Aliases = append(m.Aliases, alias)
	slices.Sort(m.Aliases)
	return true, WriteState(dir, st)
}
