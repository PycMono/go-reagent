package agenttraining

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent/application/service/agentruntime"
	"github.com/PycMono/go-reagent/application/service/agentversion"
	"github.com/PycMono/go-reagent/common/dto"
	mapping "github.com/PycMono/go-reagent/conversation"
	"github.com/PycMono/go-reagent/domain/entity/agent"
	training "github.com/PycMono/go-reagent/domain/entity/agenttraining"
	conversation "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/pi"
)

type RuntimeAuthor struct {
	Runtimes *agentruntime.Manager
	Bundles  BundleStore
}

func (a *RuntimeAuthor) Run(ctx context.Context, session training.Session, input dto.TrainingRunDTO, messages []*conversation.Message, listener pi.EventListener) (pi.RunResult, error) {
	root, err := a.Bundles.CandidatePath(ctx, candidate(session))
	if err != nil {
		return pi.RunResult{}, err
	}
	history, err := mapping.TrainingHistory(messages)
	if err != nil {
		return pi.RunResult{}, err
	}
	request := pi.RunRequest{History: history, Input: pi.Message{ContentType: "text", SenderType: "customer", Content: input.Content, ImageURLs: input.ImageURLs}, Context: []pi.ContextBlock{{Name: "training", Content: "You are editing a training candidate. Use training_file to inspect and change its behavior assets. Before using tools, briefly explain your intended action to the administrator in their language. Inspect relevant assets before editing; if the requested outcome is ambiguous, ask a concise question. After editing, summarize the actual changes and verification. Do not expose private internal reasoning. Validation and publication require administrator actions; never claim to have published."}}}
	return a.runCandidate(ctx, session, "training", input.RunID, root, request, listener)
}

func (a *RuntimeAuthor) Preview(ctx context.Context, session training.Session, input dto.TrainingPreviewDTO, operation, root string) (pi.RunResult, error) {
	request := pi.RunRequest{Input: pi.Message{ContentType: "text", SenderType: "customer", Content: input.Content}}
	for _, message := range input.History {
		sender := "customer"
		if message.Role == "assistant" {
			sender = "ai"
		}
		request.History = append(request.History, pi.Message{ContentType: "text", SenderType: sender, Content: message.Content})
	}
	return a.runCandidate(ctx, session, "preview", operation, root, request, nil)
}

func (a *RuntimeAuthor) runCandidate(ctx context.Context, session training.Session, kind, operation, root string, request pi.RunRequest, listener pi.EventListener) (pi.RunResult, error) {
	var result pi.RunResult
	snapshot, err := agentversion.ParseSnapshot(session.CandidateConfig)
	if err != nil {
		return result, err
	}
	request.Limits = snapshot.Runtime.Limits
	if limit := snapshot.Runtime.HistoryMessageLimit; limit > 0 && len(request.History) > limit {
		request.History = request.History[len(request.History)-limit:]
	}
	// The descriptor keys an ephemeral candidate runtime; it is never stored as
	// a production version and can never be used to materialize production files.
	model, _ := json.Marshal(snapshot.Model)
	tools, _ := json.Marshal(snapshot.Tools)
	runtime, _ := json.Marshal(snapshot.Runtime)
	headKey := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(session.CandidateHead)))
	digest, err := agentversion.SpecDigest(headKey, snapshot)
	if err != nil {
		return result, err
	}
	v := agent.Version{ID: session.BaseVersionID, TenantID: session.TenantID, AgentID: session.AgentID, BundleDigest: headKey, SpecDigest: digest, ModelConfig: model, ToolPolicy: tools, RuntimeConfig: runtime, Validation: json.RawMessage(`{"digest_version":1}`)}
	lease, err := a.Runtimes.Acquire(ctx, agentruntime.Request{Key: agentruntime.Key{Kind: kind, TenantID: session.TenantID, AgentID: session.AgentID, ConversationID: session.ConversationID, VersionID: v.ID, OperationID: operation, SpecDigest: digest}, Version: v, CandidateRoot: root})
	if err != nil {
		return result, err
	}
	defer lease.Release()
	result, err = lease.Runner.Run(ctx, request, listener)
	return result, errors.Join(err, lease.Stop())
}
