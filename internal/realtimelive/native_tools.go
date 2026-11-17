package realtimelive

import (
	"context"
	"errors"
	"time"
)

type plannedControlCall struct{ id, name, args string }

func controlCallFields(item map[string]any, allowed map[string]bool) (plannedControlCall, error) {
	id, iok := item["call_id"].(string)
	name, nok := item["name"].(string)
	args, aok := item["arguments"].(string)
	if !iok || !nok || !aok || len(id) == 0 || len(id) > 256 || len(args) > 2048 || !allowed[name] {
		return plannedControlCall{}, errors.New("tool_call_fields")
	}
	return plannedControlCall{id, name, args}, nil
}

func (n *Native) completedControlCalls(ctx context.Context, allowed, seen map[string]bool) (calls []plannedControlCall, routeErr error) {
	defer func() {
		if auditErr := n.finishPlanning(ctx, routeErr); auditErr != nil {
			routeErr = auditErr
		}
	}()
	identity := ""
	arguments := map[string]plannedControlCall{}
	for range 256 {
		e, err := n.Read(ctx)
		if err != nil {
			return nil, err
		}
		switch e["type"] {
		case "response.created":
			id, ok := object(e["response"])["id"].(string)
			if identity != "" || !ok || id == "" || len(id) > 256 {
				return nil, errors.New("tool_response_identity")
			}
			identity = id
		case "response.function_call_arguments.done":
			if identity == "" || e["response_id"] != identity {
				return nil, errors.New("tool_response_identity")
			}
			call, err := controlCallFields(e, allowed)
			if err != nil {
				return nil, err
			}
			if !allowed[call.name] || seen[call.id] || arguments[call.id].id != "" || len(arguments) >= 8 {
				return nil, errors.New("duplicate_or_unknown_tool")
			}
			arguments[call.id] = call
		case "response.done":
			r := object(e["response"])
			if identity == "" || r["id"] != identity {
				return nil, errors.New("tool_response_terminal")
			}
			if n.OnToolTerminal != nil {
				if err := n.OnToolTerminal(r); err != nil {
					return nil, err
				}
			}
			if err := n.recordPlanningUsage(r); err != nil {
				return nil, err
			}
			if r["status"] != "completed" {
				return nil, errors.New("tool_response_terminal")
			}
			output, ok := r["output"].([]any)
			if !ok || len(output) > 16 {
				return nil, errors.New("tool_response_output")
			}
			var calls []plannedControlCall
			for _, raw := range output {
				item := object(raw)
				if item["type"] != "function_call" {
					continue
				}
				call, err := controlCallFields(item, allowed)
				if err != nil {
					return nil, err
				}
				if !allowed[call.name] || seen[call.id] || arguments[call.id] != call {
					return nil, errors.New("tool_response_mismatch")
				}
				delete(arguments, call.id)
				calls = append(calls, call)
			}
			if len(arguments) != 0 {
				return nil, errors.New("tool_response_incomplete")
			}
			return calls, nil
		}
	}
	return nil, errors.New("tool_event_limit")
}

// A matching terminal is enough to retain billing evidence even if its tool
// output is invalid. Missing or partial provider usage remains explicitly unknown.
func (n *Native) recordPlanningUsage(response map[string]any) error {
	n.writeMu.Lock()
	defer n.writeMu.Unlock()
	if n.audit == nil || n.planningRequests == 0 {
		return nil
	}
	usage := expressionUsage(response)
	state := "unknown"
	_, input := usage["input_tokens"]
	_, output := usage["output_tokens"]
	_, total := usage["total_tokens"]
	if input && output && total {
		state = "complete"
	}
	err := n.audit.update(map[string]any{"response_id": response["id"], "usage": usage, "usage_state": state, "planning_ms": time.Since(n.planningStarted).Milliseconds()})
	if err == nil {
		n.planningTerminal = true
	}
	return err
}

// This reader owns the explicit planning exit. Close only releases the socket;
// repeated or later closes cannot overwrite a matched terminal measurement.
func (n *Native) finishPlanning(ctx context.Context, routeErr error) error {
	n.writeMu.Lock()
	defer n.writeMu.Unlock()
	if n.audit == nil || n.planningRequests == 0 {
		return nil
	}
	outcome := "response_completed"
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		outcome = "deadline_exceeded"
	case ctx.Err() != nil:
		outcome = "canceled"
	case routeErr != nil:
		outcome = "failed"
	}
	fields := map[string]any{"planning_outcome": outcome}
	if !n.planningTerminal {
		fields["planning_ms"] = time.Since(n.planningStarted).Milliseconds()
	}
	return n.audit.update(fields)
}
