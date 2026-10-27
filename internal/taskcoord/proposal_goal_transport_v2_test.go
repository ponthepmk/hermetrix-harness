package taskcoord

import (
	"context"
	"encoding/json"
	"fmt"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/worker"
)

type boundedGoalAdapter struct {
	Base          *goalAdapter
	Inputs        []worker.Task
	ProposalWires []string
	Reviews       []worker.ReviewTask
	ReviewWires   []string
}

func (r *boundedGoalAdapter) StreamChat(ctx context.Context, _ providers.Profile, _ string, req providers.ChatRequest, _ func(providers.Delta) error) (providers.Completion, error) {
	if len(req.Tools) == 0 || len(req.Messages) < 2 || r.Base == nil {
		return providers.Completion{}, fmt.Errorf("bounded goal transport requires tools, messages, and base adapter")
	}

	name := req.Tools[0].Function.Name
	switch name {
	case "submit_plan":
		return boundedGoalPlan(ctx, r.Base, providers.Profile{}, "", req, nil)
	case "submit_changes":
		r.Base.proposals++
		var input worker.Task
		if err := json.Unmarshal([]byte(req.Messages[1].Content), &input); err != nil {
			return providers.Completion{}, err
		}
		r.Inputs = append(r.Inputs, input)
		r.ProposalWires = append(r.ProposalWires, req.Messages[1].Content)
		return boundedGoalChanges()
	case "submit_review":
		var review worker.ReviewTask
		if err := json.Unmarshal([]byte(req.Messages[1].Content), &review); err != nil {
			return providers.Completion{}, err
		}
		r.Reviews = append(r.Reviews, review)
		r.ReviewWires = append(r.ReviewWires, req.Messages[1].Content)
		return r.Base.StreamChat(ctx, providers.Profile{}, "", req, nil)
	default:
		return r.Base.StreamChat(ctx, providers.Profile{}, "", req, nil)
	}
}
