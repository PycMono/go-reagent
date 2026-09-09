package harness

import (
	"context"
	"crypto/sha256"
	"fmt"

	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/harness/skills"
)

// WorkspaceReport summarizes the workspace inputs used to build an Agent context.
type WorkspaceReport struct {
	AgentInstructionsDigest string
	Skills                  []skills.Summary
	Diagnostics             []skills.Diagnostic
}

type workspaceSnapshot struct {
	agents []byte
	skills *skills.Snapshot
}

// InspectWorkspace parses a workspace without creating a runtime or changing workspace files.
func InspectWorkspace(ctx context.Context, workDir string) (WorkspaceReport, error) {
	snapshot, err := loadWorkspaceSnapshot(ctx, workDir)
	if err != nil {
		return WorkspaceReport{}, err
	}
	digest := sha256.Sum256(snapshot.agents)
	return WorkspaceReport{
		AgentInstructionsDigest: fmt.Sprintf("sha256:%x", digest),
		Skills:                  snapshot.skills.Skills(),
		Diagnostics:             snapshot.skills.Diagnostics(),
	}, nil
}

func loadWorkspaceSnapshot(ctx context.Context, workDir string) (workspaceSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return workspaceSnapshot{}, err
	}
	agents, err := NewPromptComposer(workDir).loadAgentsInstructions()
	if err != nil {
		return workspaceSnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return workspaceSnapshot{}, err
	}
	snapshot, err := skills.Discover(workDir)
	if err != nil {
		return workspaceSnapshot{}, fmt.Errorf("%w: 发现 Agent Skills 失败: %w", pierrors.ErrWorkspaceInvalid, err)
	}
	if err := ctx.Err(); err != nil {
		return workspaceSnapshot{}, err
	}
	return workspaceSnapshot{agents: agents, skills: snapshot}, nil
}
