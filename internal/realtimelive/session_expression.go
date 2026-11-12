package realtimelive

import "context"

// All per-turn expression state and transport writes are owned by Session.mu.
// Readiness is event driven: a missing final ASR transcript creates no waiter.
func (s *Session) expressionContextReady(rt *liveTurn, history string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.expressionEligibleLocked(rt) || rt.expressionStarted {
		return
	}
	rt.expressionHistory, rt.expressionContextReady = history, true
	s.maybeStartExpressionLocked(rt)
}

func (s *Session) expressionEligibleLocked(rt *liveTurn) bool {
	w, ok := s.wire.(*xiaozhiWire)
	return !rt.media && ok && w.expression && s.cfg.ExpressionsEnabled && s.ctx.Err() == nil && rt.ctx.Err() == nil && s.output == rt && !rt.turn.Cut && !rt.turn.PlaybackFinal
}

func (s *Session) maybeStartExpressionLocked(rt *liveTurn) {
	if !s.expressionEligibleLocked(rt) || !rt.expressionContextReady || !closed(rt.transcriptReady) || rt.expressionStarted {
		return
	}
	rt.expressionStarted = true
	ctx, cancel := context.WithCancel(rt.ctx)
	rt.expressionCancel = cancel
	input := ExpressionInput{CurrentTranscript: rt.turn.User, DeviceName: s.product.Name(), PlayedContext: rt.expressionHistory, AssistantPrefix: rt.turn.Generated}
	rt.expressionHistory = ""
	s.spawn(func() {
		defer cancel()
		result := s.selectExpression(ctx, input)
		// Even an injected selector cannot publish noncanonical values.
		if !canonicalExpression(result.Emotion) {
			result.Emotion, result.Reason = "neutral", "invalid_tool"
		}
		fields := map[string]any{"id": rt.turn.ID, "emotion": result.Emotion, "reason": result.Reason, "total_ms": result.TotalMS, "connect_ms": result.ConnectMS, "request_count": result.RequestCount, "usage": result.Usage}
		if err := s.record("expression_selected", fields); err != nil {
			return
		}
		s.mu.Lock()
		if ctx.Err() != nil || !s.expressionEligibleLocked(rt) {
			s.mu.Unlock()
			return
		}
		rt.expressionPending = &result
		err := s.sendExpressionLocked(rt)
		s.mu.Unlock()
		if err != nil {
			s.fail()
		}
	})
}

func (s *Session) sendExpressionLocked(rt *liveTurn) error {
	if !s.expressionEligibleLocked(rt) || !rt.turn.Begun || rt.expressionPending == nil || rt.expressionSent {
		return nil
	}
	if err := s.sendLocked(map[string]any{"type": "expression", "id": rt.turn.ID, "emotion": rt.expressionPending.Emotion}); err != nil {
		return err
	}
	rt.expressionSent = true
	rt.expressionPending = nil
	return nil
}

func (s *Session) retireExpressionLocked(rt *liveTurn) {
	if rt.expressionCancel != nil {
		rt.expressionCancel()
	}
	rt.expressionPending = nil
	rt.expressionHistory = ""
}
