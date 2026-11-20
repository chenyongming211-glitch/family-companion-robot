package realtimelive

import (
	"encoding/binary"
	"math"
)

// At 16 kHz each point averages 0.5 ms of PCM and a 64-point window spans
// 32 ms. Searching the eight reference phases preserves single-sample delays.
const echoStride = 8
const echoWindow = 64

// Reduce into caller-owned scratch space. Phase is measured in PCM samples.
func echoAverages(pcm []byte, phase int, dst []float64) []float64 {
	n := (len(pcm)/2 - phase) / echoStride
	for i := range n {
		offset := 2 * (phase + i*echoStride)
		sum := 0
		for j := range echoStride {
			sum += int(int16(binary.LittleEndian.Uint16(pcm[offset+2*j:])))
		}
		dst[i] = float64(sum) / echoStride
	}
	return dst[:n]
}

type echoAudioWindow struct {
	start  int
	points []float64 // DC removed
	energy float64
}

// A single sinusoid satisfies x[n-1]+x[n+1] = k*x[n]. Require several
// active windows with energy away from that fitted tone before matching.
func (w echoAudioWindow) broadbandEnergy() float64 {
	var xx, xy, yy float64
	for i := 1; i < len(w.points)-1; i++ {
		x, y := w.points[i], w.points[i-1]+w.points[i+1]
		xx += x * x
		xy += x * y
		yy += y * y
	}
	if xx == 0 {
		return 0
	}
	return yy - xy*xy/xx
}

// Return signed squared normalized correlation. Only order and thresholds
// matter, so the lag search does not need a square root per candidate.
func (w echoAudioWindow) correlation(ref []float64) float64 {
	var dot, sum, squares float64
	for i, x := range w.points {
		y := ref[i]
		dot += x * y
		sum += y
		squares += y * y
	}
	n := float64(len(w.points))
	energy := squares - sum*sum/n
	if energy < 8*8*n {
		return 0
	}
	return math.Copysign(dot*dot/(w.energy*energy), dot)
}

// Opus can preserve pitch and level while changing the phase of harmonics.
// In a window whose full waveform only partially agrees, check the pitch band
// and the remaining waveform independently at the same lag and capture gain.
func (w echoAudioWindow) codecBandsAgree(ref []float64, gain float64) bool {
	if len(w.points) < 24 || math.Copysign(1, gain)*w.correlation(ref) < .5 {
		return false
	}
	type band struct{ sx, sy, xx, yy, xy float64 }
	var bands [2]band
	smooth := func(p []float64, i int) float64 {
		// Nine-point Hann low pass at 2 kHz; endpoints have zero weight.
		return (.1464466*(p[i-3]+p[i+3]) + .5*(p[i-2]+p[i+2]) +
			.8535534*(p[i-1]+p[i+1]) + p[i]) / 4
	}
	for i := 4; i+4 < len(w.points); i++ {
		x, y := w.points[i], ref[i]*gain
		lowX, lowY := smooth(w.points, i), smooth(ref, i)*gain
		for j, v := range [2][2]float64{{lowX, lowY}, {x - lowX, y - lowY}} {
			b := &bands[j]
			b.sx += v[0]
			b.sy += v[1]
			b.xx += v[0] * v[0]
			b.yy += v[1] * v[1]
			b.xy += v[0] * v[1]
		}
	}
	n := float64(len(w.points) - 8)
	for i, b := range bands {
		xx, yy, xy := b.xx-b.sx*b.sx/n, b.yy-b.sy*b.sy/n, b.xy-b.sx*b.sy/n
		// Both bands must carry observable energy, and their levels must
		// be within 3 dB of playback under the same gain for the whole clip.
		if xx < .01*w.energy || yy <= 0 || xx < .5*yy || yy < .5*xx || xy <= 0 {
			return false
		}
		minScore := .5
		if i == 0 {
			minScore = .95 * .95
		}
		if xy*xy < minScore*xx*yy {
			return false
		}
	}
	return true
}

// Gating can cut through one 16 ms boundary slice. More than one active
// mismatch at an edge, or active sound entirely outside playback, vetoes
// an otherwise strong interior match. This also checks any partial last window.
func echoSlicesAgree(mic, ref []float64, offset int, gain, minPower float64, boundary bool) bool {
	const slice = echoWindow / 2
	mismatches := 0
	for start := 0; start < len(mic); start += slice {
		data := mic[start:min(start+slice, len(mic))]
		if len(data) < slice/2 {
			break
		}
		var buf [slice]float64
		var sum, energy float64
		for _, v := range data {
			sum += v
		}
		mean := sum / float64(len(data))
		for i, v := range data {
			buf[i] = v - mean
			energy += buf[i] * buf[i]
		}
		if energy < minPower*float64(len(data)) {
			continue
		}
		j := offset + start
		if j >= len(ref) || j+len(data) <= 0 {
			return false
		}
		if j >= 0 && j+len(data) <= len(ref) {
			w := echoAudioWindow{points: buf[:len(data)], energy: energy}
			if math.Copysign(1, gain)*w.correlation(ref[j:j+len(data)]) >= .92*.92 || w.codecBandsAgree(ref[j:j+len(data)], gain) {
				continue
			}
		}
		// A gated slice can contain a nearly constant leading or trailing
		// section. An energetic mismatch across the whole slice is speech
		// evidence and must be passed through, even at the capture boundary.
		quietEdge := false
		for _, edge := range [][]float64{data[:4], data[len(data)-4:]} {
			lo, hi := edge[0], edge[0]
			for _, v := range edge[1:] {
				lo, hi = min(lo, v), max(hi, v)
			}
			if (hi-lo)*(hi-lo) <= minPower {
				quietEdge = true
				break
			}
		}
		mismatches++
		if !boundary || !quietEdge || mismatches > 1 {
			return false
		}
	}
	return true
}

// A nearby reflecting surface adds one delayed copy of the same playback.
// Fit a single response over the whole interior. A local fit at the capture
// boundary could instead explain away an incoming human voice.
func echoReflection(inner []echoAudioWindow, ref []float64, offset int) (delay int, gain float64) {
	var bestReduction float64
	for lag := 1; lag <= 8; lag++ { // 0.5–4 ms
		var xx, yy, zz, xy, xz, yz float64
		for _, w := range inner {
			at := offset + w.start
			if at < lag {
				continue
			}
			var sumY, sumZ float64
			for i, x := range w.points {
				y, z := ref[at+i], ref[at+i-lag]
				sumY += y
				sumZ += z
				yy += y * y
				zz += z * z
				yz += y * z
				xy += x * y
				xz += x * z
			}
			yy -= sumY * sumY / echoWindow
			zz -= sumZ * sumZ / echoWindow
			yz -= sumY * sumZ / echoWindow
			xx += w.energy
		}
		// Least squares for a direct path and one weaker reflection, after
		// removing the per-window DC offset. Reject degenerate fits.
		det := yy*zz - yz*yz
		if yy == 0 || det <= 0.001*yy*zz {
			continue
		}
		direct := (xy*zz - xz*yz) / det
		reflected := (xz*yy - xy*yz) / det
		if direct == 0 || math.Abs(reflected) > .5*math.Abs(direct) {
			continue
		}
		singleError := xx - xy*xy/yy
		reduction := direct*xy + reflected*xz - xy*xy/yy
		if reduction > .30*singleError && reduction > bestReduction {
			delay, gain, bestReduction = lag, reflected/direct, reduction
		}
	}
	return delay, gain
}

// matchingPlaybackAudio tests for an aligned copy of playback in one short
// capture, never for speaker identity or semantic similarity. PCM16LE, mono,
// 16 kHz. False means acoustic evidence is insufficient to suppress speech.
func matchingPlaybackAudio(mic, reference []byte) bool {
	if len(mic)%2 != 0 || len(reference)%2 != 0 || len(mic) > 32000 || len(reference) > 96000 ||
		len(mic) < 2*echoStride*echoWindow*5 || len(reference) < 2*echoStride*echoWindow*3 {
		return false
	}
	x := echoAverages(mic, 0, make([]float64, len(mic)/(2*echoStride)))
	windows := make([]echoAudioWindow, 0, len(x)/echoWindow)
	var maxEnergy float64
	for start := 0; start+echoWindow <= len(x); start += echoWindow {
		points := x[start : start+echoWindow]
		var mean, energy float64
		for _, v := range points {
			mean += v
		}
		mean /= echoWindow
		for i := range points {
			points[i] -= mean
			energy += points[i] * points[i]
		}
		maxEnergy = max(maxEnergy, energy)
		windows = append(windows, echoAudioWindow{start, points, energy})
	}
	active := windows[:0]
	for _, w := range windows {
		// Ignore quiet boundaries and windows below 8% of peak RMS.
		if w.energy >= max(32*32*echoWindow, 0.08*0.08*maxEnergy) {
			active = append(active, w)
		}
	}
	if len(active) < 5 {
		return false
	}
	// The first and last active windows can straddle microphone gating.
	// A mismatch in any remaining active window vetoes the echo decision.
	inner := active[1 : len(active)-1]
	var anchor echoAudioWindow
	var bestDetail float64
	broad := 0
	for _, w := range inner {
		detail := w.broadbandEnergy()
		if detail > .02*w.energy {
			broad++
			if detail > bestDetail {
				bestDetail = detail
				anchor = w
			}
		}
	}
	if broad < 3 {
		return false
	}
	scratch := make([]float64, len(reference)/(2*echoStride))
	var filtered []float64
	start, end := inner[0].start, inner[len(inner)-1].start+echoWindow
	minPower := max(32*32, 0.08*0.08*maxEnergy/echoWindow)
	agrees := func(ref []float64, offset int) bool {
		score := anchor.correlation(ref[offset+anchor.start:])
		if math.Abs(score) < .94*.94 {
			return false
		}
		polarity := math.Copysign(1, score)
		var micEnergy, refEnergy float64
		for _, w := range inner {
			at := offset + w.start
			var sum float64
			for _, v := range ref[at : at+echoWindow] {
				refEnergy += v * v
				sum += v
			}
			refEnergy -= sum * sum / echoWindow
			micEnergy += w.energy
		}
		gain := polarity * math.Sqrt(micEnergy/refEnergy)
		partial := 0
		for _, w := range inner {
			r := ref[offset+w.start:]
			if polarity*w.correlation(r) >= .92*.92 {
				continue
			}
			partial++
			if 4*partial > len(inner) || !w.codecBandsAgree(r, gain) {
				return false
			}
		}
		return echoSlicesAgree(x[:start], ref, offset, gain, minPower, true) &&
			echoSlicesAgree(x[start:end], ref, offset+start, gain, minPower, false) &&
			echoSlicesAgree(x[end:], ref, offset+end, gain, minPower, true)
	}
	for phase := range echoStride {
		ref := echoAverages(reference, phase, scratch)
		first := anchor.start - inner[0].start
		last := len(ref) - echoWindow - (inner[len(inner)-1].start - anchor.start)
		best := 0.0
		at := first
		for j := first; j <= last; j++ {
			score := anchor.correlation(ref[j : j+echoWindow])
			if math.Abs(score) > math.Abs(best) {
				best, at = score, j
			}
		}
		offset := at - anchor.start
		if math.Abs(best) >= .94*.94 && agrees(ref, offset) {
			return true
		}
		// Only plausible alignments warrant fitting a room reflection. The
		// filtered reference must then pass exactly the same validation.
		if math.Abs(best) < .88*.88 {
			continue
		}
		delay, gain := echoReflection(inner, ref, offset)
		if delay == 0 {
			continue
		}
		if filtered == nil {
			filtered = make([]float64, len(scratch))
		}
		for i := range ref {
			filtered[i] = ref[i]
			if i >= delay {
				filtered[i] += gain * ref[i-delay]
			}
		}
		if agrees(filtered[:len(ref)], offset) {
			return true
		}
	}
	return false
}
