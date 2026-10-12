package tier

import (
	"errors"
	"fmt"

	"github.com/jitllm/jitllm/jit/gpu/backend"
	"github.com/jitllm/jitllm/jit/gpu/ir"
	"github.com/jitllm/jitllm/jit/gpu/kernels"
)

// fuseActIn builds block l's operand-prologue kernels (kernels.ActIn): q/k/v
// normalising and quantizing the residual themselves, gate and up the FFN's
// normed row, the attention output and FFN down projections quantizing their
// float input. Each takes the place of a RMSNormQuantRows or Quantize launch
// on a one-token decode, and the operand never reaches global memory.
//
// Built beside the kernels they shadow, after fuseQKV and after every
// retune, so the split each one carries is the shipping one. The launch
// decides per block whether the block's graph lets it run (actInBlock).
func (g *devTier) fuseActIn(l *layer) {
	l.qkvIn = nil
	for _, m := range []*mv{&l.mvq, &l.mvk, &l.mvv, &l.mvo, &l.mvg, &l.mvu, &l.mvd} {
		m.resIn, m.gatedIn, m.kernIn, m.resActIn = nil, nil, nil, nil
	}
	if g.bs == nil {
		return
	}
	p := &g.bs.p
	// The prologue meets its window and sums by subgroup shuffles.
	if lnBlock(p) || l.mla || l.linear || l.nonCausal || g.pickLanes() != ir.SubgroupLanes {
		return
	}
	win := max(p.ActWin, 32)
	norm := kernels.ActIn{On: true, Norm: true, Eps: float32(p.RMSEps), Warp: g.warpNorm(), Window: win}
	quant := kernels.ActIn{On: true, Window: win}
	pf := g.ActInPrefetch
	switch {
	case pf == 0:
		// Measured on a V100 (Llama-3.2-1B): 4 to 8 words beat none and
		// every word; all of them thrash L2 behind the prologue.
		pf = 8
	case pf < 0:
		pf = 0
	}
	norm.Prefetch, quant.Prefetch = pf, pf
	sites := g.ActInSites
	if sites == 0 {
		sites = actInDefault
	}
	if sites&actInQKV != 0 && l.qkv != nil && actInPays(l.qkvBlocks, l.mvq.k) {
		var segs []kernels.MatVecShape
		for _, m := range []mv{l.mvq, l.mvk, l.mvv} {
			segs = append(segs, kernels.MatVecShape{Center: g.center, T: m.q, K: m.k, Rows: m.rows, Bias: m.bias != nil})
		}
		l.qkvIn = g.segInKernel(segs, l.mvq.split, l.mvq.group, norm)
	}
	plain := func(m mv) bool {
		if m.kern == nil || m.slots != 0 || m.groups != 0 || m.threads != 0 || m.act != nil {
			return false
		}
		w := 128
		if m.group {
			w = kernels.GroupSplitWidth(m.split)
		}
		return actInPays((m.rows*max(m.split, 1)/max(m.rowt, 1)+w-1)/w, m.k)
	}
	shape := func(m mv, in kernels.ActIn) kernels.MatVecShape {
		return kernels.MatVecShape{Center: g.center, T: m.q, K: m.k, Rows: m.rows, Split: m.split,
			GroupSplit: m.group, Rowt: max(m.rowt, 1), Bias: m.bias != nil, In: in}
	}
	// q, k and v one launch each, where they are not one launch together.
	if sites&actInQKV != 0 && l.qkv == nil && plain(l.mvq) && plain(l.mvk) && plain(l.mvv) {
		l.mvq.kernIn, l.mvk.kernIn, l.mvv.kernIn = g.inKernel(shape(l.mvq, norm)), g.inKernel(shape(l.mvk, norm)),
			g.inKernel(shape(l.mvv, norm))
		if l.mvq.kernIn == nil || l.mvk.kernIn == nil || l.mvv.kernIn == nil {
			l.mvq.kernIn, l.mvk.kernIn, l.mvv.kernIn = nil, nil, nil
		}
	}
	res := func(m mv, in kernels.ActIn) backend.Kernel {
		if !plain(m) || m.res == nil || m.bias != nil {
			return nil
		}
		s := shape(m, in)
		s.Bias = true
		return g.inKernel(s)
	}
	if sites&actInO != 0 {
		l.mvo.resIn = res(l.mvo, quant)
	}
	if sites&actInDown != 0 && l.mvu.gated != nil {
		l.mvd.resIn = res(l.mvd, quant)
	}
	if sites&actInGateUp == 0 || ungatedFFN(p) || !plain(l.mvg) || !plain(l.mvu) || l.mvg.bias != nil || l.mvu.bias != nil {
		return
	}
	l.mvg.kernIn = g.inKernel(shape(l.mvg, norm))
	if l.mvu.gated != nil {
		s := shape(l.mvu, norm)
		s.Rowt, s.Gate, s.GateAct = 1, true, p.Act
		l.mvu.gatedIn = g.inKernel(s)
	}
	if l.mvu.gatedIn == nil && sites&actInDown != 0 {
		// No gated up: up writes its row, and down forms act(gate)*up as it
		// quantizes (QuantizeAct's arithmetic).
		l.mvu.kernIn = g.inKernel(shape(l.mvu, norm))
		l.mvd.resActIn = res(l.mvd, kernels.ActIn{On: true, Gated: true, Act: p.Act, Window: win, Prefetch: pf})
	}
	if l.mvg.kernIn == nil || (l.mvu.gatedIn == nil && l.mvu.kernIn == nil) {
		l.mvg.kernIn, l.mvu.gatedIn, l.mvu.kernIn = nil, nil, nil
	}
}

// actInPays is the policy for which matvecs take the prologue: every
// workgroup builds the operand, so its cost grows with groups*k. Measured on
// a V100 (decode tok/s against the two launches): Llama-3.2-1B's q/k/v (96
// groups, k 2048), attention output (64, 2048) and down (64, 8192) win;
// its gate and up (256, 2048) lose 3%; Llama-3.1-8B's attention output
// (128, 4096) is even and its down (128, 14336) loses 4%. A per-device
// measurement would replace these two numbers (the split tuner times the
// plain kernels only).
func actInPays(groups, k int) bool { return groups <= 128 && groups*k <= 512<<10 }

// The operand-prologue sites (Config.ActInSites).
const (
	actInQKV = 1 << iota
	actInO
	actInGateUp
	actInDown
	// Gate and up are not in the default: their 256 workgroups each
	// normalising the row cost more than the norm launch (V100,
	// Llama-3.2-1B: sites 11 read 557 tok/s, 15 read 541, the two launches 535).
	actInDefault = actInQKV | actInO | actInDown
)

// inKernel is an ActIn matvec from the device-wide cache (idKerns, keyed by
// the whole shape), nil where the kernels refuse the shape or it does not
// compile -- recorded in LastErr, and the block keeps its two launches.
func (g *devTier) inKernel(s kernels.MatVecShape) backend.Kernel {
	k, err := g.idKernel(s)
	if err != nil {
		g.actInDeclined(err)
		return nil
	}
	return k
}

func (g *devTier) segInKernel(segs []kernels.MatVecShape, split int, grp bool, in kernels.ActIn) backend.Kernel {
	key := fmt.Sprintf("in/%v/%d/%v/%v", segs, split, grp, in)
	if k, ok := g.segKerns[key]; ok {
		return k
	}
	ker, err := kernels.MatVecSegmentsIn(segs, split, grp, in)
	var k backend.Kernel
	if err == nil {
		k, err = g.dev.Compile(ker)
	}
	if err != nil {
		g.actInDeclined(err)
		k = nil
	}
	if g.segKerns == nil {
		g.segKerns = map[string]backend.Kernel{}
	}
	g.segKerns[key] = k
	return k
}

// actInOn is whether this submission may take the operand prologue at all.
func (g *devTier) actInOn() bool { return !g.NoFuse && !g.NoActFuse }

// actInDeclined records why a prologue kernel was not built. A width the
// kernel's registers cannot launch at (backend.ErrGroupTooWide) is a shape
// this device takes in two launches, not an error.
func (g *devTier) actInDeclined(err error) {
	if errors.Is(err, backend.ErrGroupTooWide) {
		g.ActInDeclined++
		return
	}
	g.LastErr = err.Error()
}
