// Package hubtest is a Hugging Face Hub on a test's loopback: the file list,
// the HEAD that sizes a file and ranged GETs, over files a test names. The
// apps' download gates run against it instead of the network.
package hubtest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SHA is the commit every revision resolves to.
const SHA = "0123456789abcdef0123456789abcdef01234567"

// Hub serves Files, keyed by "repo/file".
type Hub struct {
	*httptest.Server
	Files map[string][]byte
	// OnGet, when set, runs before each file GET is answered, from the
	// server's goroutine.
	OnGet func(r *http.Request)
	// Served is the file bytes sent so far.
	Served atomic.Int64
	// Gated answers every request 401, as the Hub does a gated repository
	// to a person who has not accepted its licence.
	Gated bool

	mu sync.Mutex
}

// New starts a Hub; Close stops it.
func New(files map[string][]byte) *Hub {
	h := &Hub{Files: files}
	h.Server = httptest.NewServer(http.HandlerFunc(h.serve))
	return h
}

func (h *Hub) serve(w http.ResponseWriter, r *http.Request) {
	if h.Gated {
		http.Error(w, "gated", http.StatusUnauthorized)
		return
	}
	p := r.URL.Path
	if rest, ok := strings.CutPrefix(p, "/api/models/"); ok {
		// /api/models/{repo}/revision/{rev}
		i := strings.Index(rest, "/revision/")
		if i < 0 {
			http.NotFound(w, r)
			return
		}
		repo := rest[:i]
		type sib struct {
			Name string `json:"rfilename"`
		}
		doc := struct {
			SHA      string `json:"sha"`
			Siblings []sib  `json:"siblings"`
		}{SHA: SHA}
		h.mu.Lock()
		for k := range h.Files {
			if f, ok := strings.CutPrefix(k, repo+"/"); ok {
				doc.Siblings = append(doc.Siblings, sib{f})
			}
		}
		h.mu.Unlock()
		if len(doc.Siblings) == 0 {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(doc)
		return
	}
	// /{repo}/resolve/{rev}/{file}
	i := strings.Index(p, "/resolve/")
	if i < 0 {
		http.NotFound(w, r)
		return
	}
	repo := strings.TrimPrefix(p[:i], "/")
	rest := p[i+len("/resolve/"):]
	j := strings.Index(rest, "/")
	if j < 0 {
		http.NotFound(w, r)
		return
	}
	file := rest[j+1:]
	h.mu.Lock()
	b, ok := h.Files[repo+"/"+file]
	h.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet && h.OnGet != nil {
		h.OnGet(r)
	}
	cw := &counting{ResponseWriter: w, n: &h.Served}
	http.ServeContent(cw, r, path.Base(file), time.Time{}, bytes.NewReader(b))
}

// counting counts the body bytes a response carries.
type counting struct {
	http.ResponseWriter
	n *atomic.Int64
}

func (c *counting) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	c.n.Add(int64(n))
	return n, err
}
