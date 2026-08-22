package observability

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDependencyGraphHasNoLegacyTracing 是设计 §16.4 的持续回归门禁：
// 模块图不得再出现 jaeger-client-go / opentracing-go。
func TestDependencyGraphHasNoLegacyTracing(t *testing.T) {
	output, err := exec.Command("go", "mod", "graph").Output()
	if err != nil {
		t.Skipf("无法执行 go mod graph: %v", err)
	}
	for _, banned := range []string{"jaeger-client-go", "opentracing-go"} {
		if strings.Contains(string(output), banned) {
			t.Fatalf("依赖图不得包含 %s（§16.1 已淘汰）", banned)
		}
	}
}
