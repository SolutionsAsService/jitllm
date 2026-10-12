// Package hubtest is a Hugging Face Hub on a test's loopback: the file list,
// the HEAD that sizes a file and ranged GETs, over files a test names. The
// apps' download gates run against it instead of the network.
package hubtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	// Requests is how many requests reached this Hub.
	Requests atomic.Int64
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

// serve answers from the Hub's own state only: a request selects a stored
// file by comparison against the stored names, and no byte of the request
// -- path, query or header -- is ever written into a response.
func (h *Hub) serve(w http.ResponseWriter, r *http.Request) {
	h.Requests.Add(1)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if h.Gated {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	p := r.URL.Path
	h.mu.Lock()
	keys := make([]string, 0, len(h.Files))
	for k := range h.Files {
		keys = append(keys, k)
	}
	h.mu.Unlock()
	sort.Strings(keys)

	// The file list: /api/models/{repo}/revision/{rev}.
	for _, k := range keys {
		repo := path.Dir(k)
		if !strings.HasPrefix(p, "/api/models/"+repo+"/revision/") {
			continue
		}
		type sib struct {
			Name string `json:"rfilename"`
		}
		doc := struct {
			SHA      string `json:"sha"`
			Siblings []sib  `json:"siblings"`
		}{SHA: SHA}
		for _, o := range keys {
			if path.Dir(o) == repo {
				doc.Siblings = append(doc.Siblings, sib{path.Base(o)})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(doc)
		return
	}

	// A file: /{repo}/resolve/{rev}/{file}, any revision.
	for _, k := range keys {
		repo, file := path.Dir(k), path.Base(k)
		if !strings.HasPrefix(p, "/"+repo+"/resolve/") || !strings.HasSuffix(p, "/"+file) {
			continue
		}
		h.mu.Lock()
		b := h.Files[k]
		h.mu.Unlock()
		if r.Method == http.MethodGet && h.OnGet != nil {
			h.OnGet(r)
		}
		lo, hi, ranged, ok := byteRange(r.Header.Get("Range"), int64(len(b)))
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.FormatInt(hi-lo, 10))
		if ranged {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", lo, hi-1, len(b)))
			w.WriteHeader(http.StatusPartialContent)
		}
		if r.Method == http.MethodHead {
			return
		}
		n, _ := w.Write(b[lo:hi])
		h.Served.Add(int64(n))
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

// byteRange reads a single "bytes=a-b" or "bytes=a-" range over size bytes.
// No header is the whole file.
func byteRange(v string, size int64) (lo, hi int64, ranged, ok bool) {
	if v == "" {
		return 0, size, false, true
	}
	spec, found := strings.CutPrefix(v, "bytes=")
	a, b, dash := strings.Cut(spec, "-")
	if !found || !dash || strings.Contains(spec, ",") {
		return 0, 0, false, false
	}
	lo, err := strconv.ParseInt(a, 10, 64)
	if err != nil || lo < 0 || lo >= size {
		return 0, 0, false, false
	}
	hi = size
	if b != "" {
		e, err := strconv.ParseInt(b, 10, 64)
		if err != nil || e < lo {
			return 0, 0, false, false
		}
		hi = min(e+1, size)
	}
	return lo, hi, true, true
}
