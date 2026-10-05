package agent

import (
	"context"

	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/tools"
)

func toolExec(ctx context.Context, workspace string, allowShell bool, tool string, args map[string]any) string {
	return tools.Execute(ctx, tools.Request{Workspace: workspace, AllowShell: allowShell}, tool, args)
}
