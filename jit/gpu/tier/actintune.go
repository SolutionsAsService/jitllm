package tier

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/jitllm/jitllm/jit/gpu/backend"
	"github.com/jitllm/jitllm/jit/gpu/kernels"
)

// tuneActIn decides, on this card, which operand-prologue sites run: for each
// site key (a shape: the q/k/v launch, the attention output, down) it times
// the two-launch form against the prologue over a sequence of the placed
// blocks that share it, the way a token runs them, and keeps the prologue
// only where it is faster. The verdict is cached per binary, device and
// shape, as the split tuner's is. A caller that forced sites (ActInSites)
// skips it. Called from retuneDecode, holding g.mu, outside any Session.
func (g *devTier) tuneActIn() {
	if g.ActInSites != 0 || g.bs == nil {
		return
	}
	g.actInTrial = true
	for _, l := range g.layers {
		if l != nil && l.ok {
			g.fuseActIn(l)
		}
	}
	g.actInTrial = false
	type cand struct {
		site int
		ls   []*layer
	}
	by := map[string]*cand{}
	var keys []string
	for _, l := range g.layers {
		if l == nil || !l.ok || (l.pg != nil && !l.pg.in) {
			continue
		}
		for i, key := range l.actInKeys[:3] {
			if key == "" {
				continue
			}
			c := by[key]
			if c == nil {
				c = &cand{site: [3]int{actInQKV, actInO, actInDown}[i]}
				by[key], keys = c, append(keys, key)
			}
			if len(c.ls) < 16 {
				c.ls = append(c.ls, l)
			}
		}
	}
	if g.actInWin == nil {
		g.actInWin = map[string]bool{}
	}
	for _, key := range keys {
		c := by[key]
		ck := g.tuneKey("actin-" + key)
		if v, ok := lookupCache(g.kb.cacheDir, "gpuactin", ck); ok && g.kb.tune != TuneForce {
			g.actInWin[key] = v == "1"
			continue
		}
		r, ok := g.timeActIn(c.site, c.ls)
		win := ok && r < actInMargin
		g.actInWin[key] = win
		if backend.Verbose() {
			fmt.Fprintf(os.Stderr, "tier: operand prologue %s: %.4f of the two launches over %d blocks (resolved %v): %v\n",
				key, r, len(c.ls), ok, win)
		}
		if ok && ck != "" {
			v := "0"
			if win {
				v = "1"
			}
			saveCache(g.kb.cacheDir, "gpuactin", ck, v)
		}
	}
	// The verdicts of this tune, not a sum over retunes: a retune after the
	// seam grows decides the same keys again.
	g.ActInTimed, g.ActInKept = len(keys), 0
	for _, key := range keys {
		if g.actInWin[key] {
			g.ActInKept++
		}
	}
	for _, l := range g.layers {
		if l != nil && l.ok {
			g.fuseActIn(l)
		}
	}
	g.dropGraph()
}

// actInMargin is how much faster than the two launches the prologue must
// time to be kept: the trial resolves about 1%, and a tie keeps two launches.
const actInMargin = 0.99

// timeActIn times one site's two arms over the blocks ls, interleaved (two
// launches first on even rounds, the prologue first on odd), and returns the
// median of the per-round ratios prologue/two-launch time. ok is false where
// the trial could not launch or did not resolve (IQR over splitTuneIQR).
func (g *devTier) timeActIn(site int, ls []*layer) (float64, bool) {
	tuneTrials.Add(1)
	bs := g.bs
	// launch is mvIn's and mvrun's launch on a Session: pre is the operand
	// (a and ax, or the prologue's float row and norm weight), extra the
	// bias or residual.
	launch := func(s backend.Session, m mv, k backend.Kernel, r *resident, pre []backend.Buf, out, extra backend.Buf) error {
		w := 128
		if m.group {
			w = kernels.GroupSplitWidth(m.split)
		}
		n := m.rows * max(m.split, 1)
		if m.rowt > 1 {
			n /= m.rowt
		}
		dst := out
		if m.split > 1 && !m.group {
			dst = g.partBuf
		}
		args := append(append([]backend.Buf{r.qs, r.d, r.sc}, pre...), dst)
		if extra != nil {
			args = append(args, extra)
		}
		if err := s.Launch(k, (n+w-1)/w, w, args...); err != nil {
			return err
		}
		if m.split > 1 && !m.group {
			return s.Launch(m.red, (m.rows+127)/128, 128, g.partBuf, out)
		}
		return nil
	}
	ax := []backend.Buf{bs.a, bs.ax}
	arm := func(s backend.Session, in bool) error {
		for _, l := range ls {
			switch site {
			case actInQKV:
				if !in {
					if bs.rmsQ == nil {
						return errNoBatch
					}
					if err := s.Launch(bs.rmsQ, 1, kernels.RMSNormGroup, bs.x, l.nAttn, bs.h, bs.a, bs.ax); err != nil {
						return err
					}
				}
				qkv := [3]struct {
					m   mv
					r   *resident
					out backend.Buf
				}{{l.mvq, l.wq, bs.q}, {l.mvk, l.wk, bs.k}, {l.mvv, l.wv, bs.v}}
				if l.qkv != nil {
					k, args := l.qkv, []backend.Buf{bs.a, bs.ax}
					if in {
						k, args = l.qkvIn, []backend.Buf{bs.x, l.nAttn}
					}
					for i, sg := range qkv {
						args = append(args, sg.r.qs, sg.r.d, sg.r.sc, sg.out)
						if l.qkvBias[i] {
							args = append(args, sg.m.bias)
						}
					}
					if err := s.Launch(k, l.qkvBlocks, l.qkvW, args...); err != nil {
						return err
					}
					continue
				}
				for _, p := range qkv {
					k, pre := p.m.kern, ax
					if in {
						k, pre = p.m.kernIn, []backend.Buf{bs.x, l.nAttn}
					}
					if err := launch(s, p.m, k, p.r, pre, p.out, p.m.bias); err != nil {
						return err
					}
				}
			case actInO, actInDown:
				m, r, q, src, dst, resid := l.mvo, l.wo, bs.quantQ, bs.xb, bs.x2, bs.x
				if site == actInDown {
					m, r, q, src, dst, resid = l.mvd, l.down, bs.quantF, bs.act, bs.x, bs.x2
				}
				if in {
					if err := launch(s, m, m.resIn, r, []backend.Buf{src}, dst, resid); err != nil {
						return err
					}
					continue
				}
				if q == nil {
					return errNoBatch
				}
				if err := s.Launch(q, (kernels.QuantizeThreads(m.k/32)+127)/128, 128, src, bs.a, bs.ax); err != nil {
					return err
				}
				if err := launch(s, m, m.res, r, ax, dst, resid); err != nil {
					return err
				}
			}
		}
		return nil
	}
	timed := func(in bool) (float64, error) {
		const reps = 3
		t0 := time.Now()
		var err error
		g.dev.Session(func(s backend.Session) {
			for i := 0; i < reps && err == nil; i++ {
				err = arm(s, in)
			}
			if err == nil {
				err = s.Sync()
			}
		})
		return time.Since(t0).Seconds(), err
	}
	const rounds = 9
	var ratios []float64
	for round := 0; round < rounds+1; round++ {
		var a, b float64
		var ea, eb error
		if round%2 == 0 {
			a, ea = timed(false)
			b, eb = timed(true)
		} else {
			b, eb = timed(true)
			a, ea = timed(false)
		}
		if ea != nil || eb != nil {
			return 0, false
		}
		if round == 0 || a <= 0 {
			continue // the first round pays first-touch
		}
		ratios = append(ratios, b/a)
	}
	sort.Float64s(ratios)
	med := ratios[len(ratios)/2]
	iqr := ratios[len(ratios)*3/4] - ratios[len(ratios)/4]
	return med, med > 0 && iqr/med <= splitTuneIQR
}
