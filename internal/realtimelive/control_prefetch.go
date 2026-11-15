package realtimelive

import (
	"context"
	"errors"
	"familyrobot/internal/realtimestate"
	"reflect"
	"time"
)

// A candidate owns only one silent plan. Its original planning and operation
// deadlines also bound the sole replan, cancellation and deferred join.
type controlPrefetch struct {
	ctx      context.Context
	cancel   context.CancelFunc
	deadline time.Time
	done     chan struct{}
	basis    controlPlanBasis
	tools    []map[string]any
	result   ControlResult
	err      error
}
type controlPlanBasis struct {
	floor int64
	input ControlInput
}
type controlDispatchGuardKey struct{}

// Called only at the MCP write point while Session.mu is held. Ordinary calls
// have no guard and retain the existing dispatch path.
func controlDispatchAllowed(ctx context.Context) bool {
	guard, _ := ctx.Value(controlDispatchGuardKey{}).(func() bool)
	return guard == nil || guard()
}
func (p *controlPrefetch) close() {
	if p != nil {
		p.cancel()
		<-p.done
	}
}

// Session.mu held. Rebuild only the existing planner projection; never use a
// stale settled string after a floor reset, late playback or private revocation.
func (s *Session) controlBasisLocked(rt *liveTurn) (controlPlanBasis, bool) {
	if !s.musicCurrent(rt) || rt.turn.ID < s.memoryFloor {
		return controlPlanBasis{}, false
	}
	s.authorizeMusicLocked()
	turns := []*realtimestate.Turn{}
	for _, old := range s.history {
		if old != rt && !old.media && old.turn.ID >= s.memoryFloor {
			turns = append(turns, old.turn)
		}
	}
	history := realtimestate.ContextFor(turns)
	if s.music != nil && s.music.contextRevoked {
		history = realtimestate.ContextFor(nil)
	}
	records := recentControlRecords(s.controls.recent, rt.turn.ID, time.Now())
	for i := range records {
		records[i].Operations = append([]ControlOperation(nil), records[i].Operations...)
	}
	name := ""
	if s.product != nil {
		name = s.product.Name()
	}
	return controlPlanBasis{s.memoryFloor, ControlInput{CurrentRequest: rt.turn.User, PlayedContext: controlHistory(history), Turn: rt.turn.ID, VerifiedControls: records, DeviceName: name}}, true
}
func (s *Session) prefetchControls(rt *liveTurn) *controlPrefetch {
	if !s.cfg.ParallelPlanning || s.controls == nil || !s.musicEnabled() {
		return nil
	}
	s.mu.Lock()
	ready := closed(rt.transcriptReady) && rt.turn.User != ""
	name := ""
	if s.product != nil {
		name = s.product.Name()
	}
	if !ready || memoryClearCommand(rt.turn.User, name) {
		s.mu.Unlock()
		return nil
	}
	basis, ok := s.controlBasisLocked(rt)
	s.mu.Unlock()
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(rt.ctx, 18*time.Second)
	p := &controlPrefetch{ctx: ctx, cancel: cancel, deadline: time.Now().Add(ControlSemanticBudget), done: make(chan struct{}), basis: basis}
	go func() { defer close(p.done); s.planPrefetchedControls(p) }()
	return p
}
func (s *Session) planPrefetchedControls(p *controlPrefetch) {
	p.result = ControlResult{Outcome: "failed_or_unknown", NoActionIssued: true}
	planning, stop := context.WithDeadline(p.ctx, p.deadline)
	defer stop()
	p.tools, p.err = s.controls.Tools(planning)
	if p.err != nil {
		p.result.Outcome = "discovery_unavailable"
		return
	}
	if len(p.tools) == 0 {
		p.result.Outcome = "unavailable"
		return
	}
	n := NewNative(s.cfg, s.directory)
	defer n.Close()
	started := time.Now()
	p.result.Plan, p.err = n.semanticPlan(planning, p.basis.input, p.tools)
	p.result.PlanMS = time.Since(started).Milliseconds()
}
func (s *Session) routePrefetchedControls(rt *liveTurn, p *controlPrefetch) (string, error) {
	<-p.done
	s.mu.Lock()
	current, valid := s.controlBasisLocked(rt)
	s.mu.Unlock()
	if !valid {
		return "", context.Canceled
	}
	if !reflect.DeepEqual(current, p.basis) {
		p.basis = current
		// Reuse the original absolute budget; invalidation grants no fresh timeout.
		s.planPrefetchedControls(p)
	}
	if p.err == nil && p.result.Plan != nil {
		guarded := context.WithValue(p.ctx, controlDispatchGuardKey{}, func() bool {
			current, valid := s.controlBasisLocked(rt)
			return valid && reflect.DeepEqual(current, p.basis)
		})
		// Recheck even terminal none/clarify plans before settling their result.
		s.mu.Lock()
		valid = controlDispatchAllowed(guarded)
		currentValid := true
		if !valid {
			_, currentValid = s.controlBasisLocked(rt)
		}
		s.mu.Unlock()
		if !valid {
			if !currentValid {
				return "", context.Canceled
			}
			// The sole replan also became stale, but this input still needs its
			// main answer. Discard only control authority; never replan again.
			p.result.Plan = nil
			p.err = errors.New("control_plan_invalidated")
			return s.controlRouteResult(rt, p.result, p.err)
		}
		p.err = runPlannedControl(guarded, p.result.Plan, func(ctx context.Context, id, name, args string) (string, error) {
			return s.controls.Call(ctx, uint64(rt.turn.ID), id, name, args)
		}, &p.result)
	}
	return s.controlRouteResult(rt, p.result, p.err)
}
