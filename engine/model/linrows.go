package model

// A prompt chunk through a recurrent block.
//
// Only the recurrence is sequential: each row's state depends on the row
// before it, and so does the short convolution's window. The projections into
// the block and out of it read nothing but the normed residual and the gated
// output, so over a chunk they are GEMMs: the weights cross the memory bus
// once per chunk instead of once per row. linearPre runs the input
// projections over the chunk, the row loop runs the convolution, the gates,
// the recurrence and the gated norm per row reading row r of each
// (lproj, lrow), and linearPost runs the output projection over the gated
// rows the loop left (lout).
//
// Mamba-1's dt, B and C projections read the convolved x, which is row
// sequential, so they stay matvecs inside the loop.
//
// WithLinearRowByRow keeps every projection a matvec per row, decode's form;
// it is the arm the batched one is held to (TestLinearRowsMatchRowByRow).

// linRows is one chunk's projections for the block being run. on is set only
// between linearPre and linearPost.
type linRows struct {
	on bool
	r  int
	// ref is WithLinearRowByRow, copied when the State is made; a test sets
	// it on one of two States over one model.
	ref bool
	// mixed is the convolved projection (wq, or ssm_in); z the output gate
	// (or KDA's gate through its rank-kDim pair, or the short convolution's
	// B); braw beta's logits (qwen3next's interleaved ba, KDA's b, Mamba-2's
	// dt, the short convolution's C); alpha KDA's forget gate; low and low2
	// KDA's two rank-kDim intermediates.
	mixed, z, braw, alpha, low, low2 []float32
	// zo is each row's gated output, the output projection's input, and
	// out the n rows it writes.
	zo []float32
	// traceName is the name the block traces its output under.
	traceName string
	// chunks counts the block-chunks run batched, so a gate can assert the
	// arm it compared was the one selected.
	chunks int64
}

// linBuf is buf at n*w floats, grown when short. A prompt's buffers live for
// the State, so a second chunk allocates nothing.
func linBuf(buf *[]float32, n, w int) []float32 {
	if len(*buf) < n*w {
		*buf = make([]float32, n*w)
	}
	return (*buf)[:n*w]
}

// linearBatched reports whether a chunk's recurrent block runs its
// projections as GEMMs.
func (s *State) linearBatched() bool { return !s.lin.ref }

// linearPre runs layer li's input projections over the n rows of h.
func (s *State) linearPre(l *layer, h []float32, n int) error {
	g, b := s.dg, &s.lin
	mm := func(dst *[]float32, w tensor, x []float32) error {
		return s.mm(linBuf(dst, n, w.rows), w, x[:n*w.k], n)
	}
	in := l.wq
	if g.ssd {
		in = l.mixIn()
	}
	if err := mm(&b.mixed, in, h); err != nil {
		return err
	}
	switch {
	case g.mamba1:
		if err := mm(&b.z, l.ssmGate, h); err != nil {
			return err
		}
	case g.chanDecay:
		if l.ssmGA.rows == 0 {
			if err := mm(&b.z, l.ssmGate, h); err != nil {
				return err
			}
		} else {
			if err := mm(&b.low, l.ssmGA, h); err != nil {
				return err
			}
			if err := mm(&b.z, l.ssmGB, b.low); err != nil {
				return err
			}
		}
		if err := mm(&b.low2, l.ssmFA, h); err != nil {
			return err
		}
		if err := mm(&b.alpha, l.ssmFB, b.low2); err != nil {
			return err
		}
		if err := mm(&b.braw, l.ssmBA, h); err != nil {
			return err
		}
	default:
		// qwen3next, Mamba-2 and the short convolution: a gate and one more.
		if err := mm(&b.z, l.ssmGate, h); err != nil {
			return err
		}
		if err := mm(&b.braw, l.ssmBA, h); err != nil {
			return err
		}
	}
	linBuf(&b.zo, n, l.ssmOut.k)
	b.on, b.r = true, 0
	b.chunks++
	s.jit.NewInput()
	return nil
}

// linearPost runs the output projection over the n gated rows into out.
func (s *State) linearPost(li int, l *layer, out []float32, n int) error {
	b := &s.lin
	b.on = false
	if err := s.mm(out[:n*l.ssmOut.rows], l.ssmOut, b.zo[:n*l.ssmOut.k], n); err != nil {
		return err
	}
	if s.m.tracer != nil {
		w := l.ssmOut.rows
		for r := 0; r < n; r++ {
			s.m.trace(li, b.traceName, out[r*w:(r+1)*w])
		}
	}
	return nil
}

// lproj is one row's projection of h through w: row r of pre when the chunk
// ran it as a GEMM, a matvec otherwise.
func (s *State) lproj(dst, pre []float32, w tensor, h []float32) error {
	if s.lin.on {
		s.lrow(dst, pre, w.rows)
		return nil
	}
	return s.mv(dst, w, h)
}

// lrow copies the current row of pre, w wide, into dst.
func (s *State) lrow(dst, pre []float32, w int) {
	copy(dst[:w], pre[s.lin.r*w:(s.lin.r+1)*w])
}

// lout is the block's output projection of z into out: a matvec, or z kept as
// row r of the chunk's output GEMM input.
func (s *State) lout(li int, name string, out []float32, w tensor, z []float32) error {
	if s.lin.on {
		copy(s.lin.zo[s.lin.r*w.k:(s.lin.r+1)*w.k], z[:w.k])
		s.lin.traceName = name
		return nil
	}
	if err := s.mv(out, w, z); err != nil {
		return err
	}
	s.m.trace(li, name, out)
	return nil
}
