package tools

import (
	"context"
	"errors"
	"fmt"
	"io"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/internal/config"
	"go.uber.org/fx"
)

// Register provides the complete workspace tool runtime.
var Register = fx.Options(
	fx.Provide(NewRuntimeRegistry),
)

// NewRuntimeRegistry creates and registers every workspace tool, then binds
// their resources to the Fx lifecycle.
func NewRuntimeRegistry(lifecycle fx.Lifecycle, workDir config.WorkDir) (Registry, error) {
	var closers toolClosers
	readFileTool, err := NewReadFileTool(string(workDir))
	if err != nil {
		return nil, fmt.Errorf("初始化 read_file 工具失败: %w", err)
	}
	closers = append(closers, readFileTool)

	editFileTool, err := NewEditFileTool(string(workDir))
	if err != nil {
		_ = closers.Close()
		return nil, fmt.Errorf("初始化 edit_file 工具失败: %w", err)
	}
	closers = append(closers, editFileTool)

	writeFileTool, err := NewWriteFileTool(string(workDir))
	if err != nil {
		_ = closers.Close()
		return nil, fmt.Errorf("初始化 write_file 工具失败: %w", err)
	}
	closers = append(closers, writeFileTool)

	applyPatchTool, err := NewApplyPatchTool(string(workDir))
	if err != nil {
		_ = closers.Close()
		return nil, fmt.Errorf("初始化 apply_patch 工具失败: %w", err)
	}
	closers = append(closers, applyPatchTool)

	processManager, err := NewProcessManager(string(workDir))
	if err != nil {
		_ = closers.Close()
		return nil, fmt.Errorf("初始化 exec/process 工具失败: %w", err)
	}
	closers = append(closers, processManager)

	runtimeTools := []Tool{
		readFileTool,
		editFileTool,
		writeFileTool,
		applyPatchTool,
		NewExecTool(processManager),
		NewProcessTool(processManager),
	}
	registry, err := NewRegistry(RegistryParams{
		Tools:       runtimeTools,
		Middlewares: defaultMiddlewareRegistrations(),
	})
	if err != nil {
		_ = closers.Close()
		return nil, fmt.Errorf("初始化工具 Registry 失败: %w", err)
	}

	lifecycle.Append(fx.Hook{OnStop: func(ctx context.Context) error {
		if err := closers.Close(); err != nil {
			logsdk.Error(ctx, "关闭工具 Registry 资源失败",
				logsdk.Any("component", "bootstrap"),
				logsdk.Err(err),
			)
			return err
		}
		return nil
	}})
	return registry, nil
}

type toolClosers []io.Closer

func (closers toolClosers) Close() error {
	closeErrors := make([]error, 0, len(closers))
	for index := len(closers) - 1; index >= 0; index-- {
		closeErrors = append(closeErrors, closers[index].Close())
	}
	return errors.Join(closeErrors...)
}
