package discover

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jitllm/jitllm/common/convertjob"
	"github.com/jitllm/jitllm/common/session"
	"github.com/jitllm/jitllm/convert"
	"github.com/jitllm/jitllm/convert/gguf"
	"github.com/jitllm/jitllm/convert/hf"
	"github.com/jitllm/jitllm/convert/library"
	"github.com/jitllm/jitllm/format/jlm"
)

// Step is one report from [Get]: what it is doing, how far, in words for the
// line beside the bar.
type Step struct {
	// Stage is "streaming", "downloading" or "converting".
	Stage string
	// Fraction is the bar, 0 to 0.99 until the model is ready.
	Fraction float64
	Label    string
}

// Hub is where [Get] reads from: the Hub's address ("" for huggingface.co)
// and the token (hf.FindToken), "" for none.
type Hub struct {
	Endpoint string
	Token    string
	// Fetch is the whole-file download the fallback takes; nil is [HubFetch].
	Fetch FetchFunc
	// Convert runs the fallback's conversion of a downloaded file, and
	// reports false when it could not: the apps put it on the engine's
	// queue, where it cannot overlap a decode. nil runs it in place. A stream
	// is not put there: it runs at the network's pace, for as long as the
	// download takes.
	Convert func(job func()) bool
	// NoStream downloads first, as a platform that cannot stream does, so
	// that path is testable everywhere.
	NoStream bool
}

// Get turns library model m into its container in dir and returns the
// conversion's summary line. progress, which may be nil, hears every step
// from Get's goroutine.
//
// The GGUF is converted where it lives (convert.StreamGGUFs, the CLI's
// streamed conversion for a GGUF): read by range from the Hub as the writer
// reaches each tensor and never stored, so the download and the conversion
// overlap and the container is the only file that grows -- the disk a model
// takes is its container's, not the GGUF's beside it. Cancelling ctx stops it
// at the next range; the container's journal keeps what was written, and the
// next Get of the same model resumes there.
//
// A GGUF already in dir is converted from the disk and kept, and where a
// stream cannot run (gguf.ErrNoStream, convert.ErrStreamHolds) the model is
// downloaded, converted and the download removed, as before.
func Get(ctx context.Context, m library.Model, dir string, hub Hub, progress func(Step)) (string, error) {
	if progress == nil {
		progress = func(Step) {}
	}
	dst := ContainerFor(dir, m)
	if !hasSource(m, dir) && gguf.Streamable() && !hub.NoStream {
		line, err := stream(ctx, m, dst, hub, progress)
		if !errors.Is(err, gguf.ErrNoStream) && !errors.Is(err, convert.ErrStreamHolds) {
			return line, err
		}
	}
	return fetchThenConvert(ctx, m, dir, dst, hub, progress)
}

// hasSource reports whether m's GGUF is in dir already.
func hasSource(m library.Model, dir string) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.Base(m.File)))
	return err == nil
}

// Streamed is what [Get] needs on the disk: the container, with a quarter to
// spare (a container can be larger than its source). [Need] is the download's.
func Streamed(m library.Model) int64 { return m.Bytes + m.Bytes/4 }

// cancellable is a Hub file whose reads stop once ctx is done.
type cancellable struct {
	ctx context.Context
	*hf.Remote
}

func (c cancellable) ReadAt(p []byte, off int64) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.Remote.ReadAt(p, off)
}

func stream(ctx context.Context, m library.Model, dst string, hub Hub, progress func(Step)) (string, error) {
	cl := &hf.Client{Endpoint: hub.Endpoint, Token: hub.Token}
	open := func(r hf.Ref) (*hf.Remote, error) {
		pinned, _, err := cl.Pin(r)
		if err != nil {
			return nil, err
		}
		pinned.File = r.File
		return cl.OpenRemote(pinned)
	}
	src, err := open(m.Ref())
	if err != nil {
		return "", err
	}
	remotes := []*hf.Remote{src}
	var tower convert.Remote
	if tr, ok := m.TowerRef(); ok {
		t, err := open(tr)
		if err != nil {
			return "", err
		}
		remotes = append(remotes, t)
		tower = cancellable{ctx, t}
	}
	var total int64
	for _, r := range remotes {
		total += r.Size()
	}
	if n, ok := jlm.Resumable(dst); ok {
		progress(Step{Stage: "streaming", Label: fmt.Sprintf("resuming %s: %d tensor(s) already written", m.Title, n)})
	}

	progress(Step{Stage: "streaming", Label: "streaming " + m.Title})
	stop := make(chan struct{})
	polled := make(chan struct{})
	go func() {
		defer close(polled)
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
			}
			var got int64
			for _, r := range remotes {
				got += r.Transferred()
			}
			progress(Step{Stage: "streaming", Fraction: convertjob.Fraction(got, total),
				Label: fmt.Sprintf("streaming %s: %s of %s read and converted",
					m.Title, session.Bytes(uint64(got)), session.Bytes(uint64(total)))})
		}
	}()
	host, _ := os.Hostname()
	t0 := time.Now()
	h, err := convert.StreamGGUFs(cancellable{ctx, src}, tower, dst, jlm.Fingerprint{Host: host})
	close(stop)
	<-polled
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	var dstBytes int64
	if fi, err := os.Stat(dst); err == nil {
		dstBytes = fi.Size()
	}
	line := convertjob.Summary(h, dst, total, dstBytes, time.Since(t0))
	progress(Step{Stage: "converting", Fraction: 1, Label: line})
	return line, nil
}

// fetchThenConvert is the whole-file path: download, convert, remove the
// download.
func fetchThenConvert(ctx context.Context, m library.Model, dir, dst string, hub Hub, progress func(Step)) (string, error) {
	given := hub.Fetch
	if given == nil {
		given = HubFetch
	}
	// The download reads from the same Hub the stream would have: a given
	// fetch that went elsewhere would turn this Hub's refusal into another's
	// answer.
	fetch := func(cl *hf.Client, r hf.Ref, dir string) (string, error) {
		cl.Endpoint = hub.Endpoint
		return given(cl, r, dir)
	}
	got, err := Download(m, dir, hub.Token, fetch, func(f float64) {
		progress(Step{Stage: "downloading", Fraction: f, Label: fmt.Sprintf("%.0f%% of %s", 100*f, m.Title)})
	})
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	run := hub.Convert
	if run == nil {
		run = func(job func()) bool { job(); return true }
	}
	var line string
	if !run(func() {
		line, err = convertjob.Run(got.Request(dst), func(u convertjob.Update) {
			if u.Stage == convertjob.Started || u.Stage == convertjob.Writing {
				progress(Step{Stage: "converting", Fraction: u.Fraction, Label: u.Label})
			}
		})
	}) {
		return "", errors.New("the engine is busy")
	}
	if err != nil {
		return "", err
	}
	got.Cleanup()
	progress(Step{Stage: "converting", Fraction: 1, Label: line})
	return line, nil
}
