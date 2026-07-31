package tools

import (
	"fmt"

	"github.com/PycMono/go-reagent/internal/config"
	"go.uber.org/fx"
)

// Register provides the complete workspace tool runtime.
var Register = fx.Options(
	fx.Provide(NewWorkspace, newRuntimeRegistry),
)

// NewRuntimeRegistry creates and registers every workspace tool, then binds
// their resources to the Fx lifecycle.
func NewRuntimeRegistry(lifecycle fx.Lifecycle, workDir config.WorkDir) (Registry, error) {
	workspace, err := NewWorkspace(lifecycle, workDir)
	if err != nil {
		return nil, err
	}
	return newRuntimeRegistry(lifecycle, workspace)
}

func newRuntimeRegistry(lifecycle fx.Lifecycle, workspace *Workspace) (Registry, error) {
	readTool, err := NewReadTool(workspace)
	if err != nil {
		_ = workspace.Close()
		return nil, fmt.Errorf("初始化 read 工具失败: %w", err)
	}

	editTool := NewEditTool(workspace)

	writeTool, err := NewWriteTool(workspace)
	if err != nil {
		_ = workspace.Close()
		return nil, fmt.Errorf("初始化 write 工具失败: %w", err)
	}

	applyPatchTool := NewApplyPatchTool(workspace)

	processSupervisor, err := NewProcessSupervisor(lifecycle, workspace)
	if err != nil {
		_ = workspace.Close()
		return nil, fmt.Errorf("初始化 exec/process 工具失败: %w", err)
	}

	runtimeTools := []Tool{
		readTool,
		editTool,
		writeTool,
		applyPatchTool,
		NewExecTool(processSupervisor),
		NewProcessTool(processSupervisor),
	}
	registry, err := NewRegistry(RegistryParams{
		Tools:       runtimeTools,
		Middlewares: defaultMiddlewareRegistrations(),
	})
	if err != nil {
		_ = processSupervisor.Close()
		_ = workspace.Close()
		return nil, fmt.Errorf("初始化工具 Registry 失败: %w", err)
	}
	return registry, nil
}
