package observability

import "context"

// generationHintKey 是 GenerationHint 的私有强类型 Context Key（§7）。
type generationHintKey struct{}

// GenerationHint 是 pi 在每次 provider.Stream 前写入的生成上下文提示。
// 它不序列化、不跨进程、不携带内容或业务 ID（§7）。
type GenerationHint struct {
	Phase        string // thinking | action | compaction
	Attempt      int    // 当前逻辑生成内从 1 开始
	RequestIndex uint32 // 当前 Run 内每次物理请求前递增
}

// WithGenerationHint 把 Hint 写入 Context；仅 pi 生成流程调用。
func WithGenerationHint(ctx context.Context, hint GenerationHint) context.Context {
	return context.WithValue(ctx, generationHintKey{}, hint)
}

// GenerationHintFrom 读取 Hint；缺失时使用 phase=unknown、attempt=1，
// RequestIndex 由第二个返回值指示是否存在（缺失时省略该属性）。
func GenerationHintFrom(ctx context.Context) (GenerationHint, bool) {
	hint, ok := ctx.Value(generationHintKey{}).(GenerationHint)
	if !ok {
		return GenerationHint{Phase: string(GenerationPhaseUnknown), Attempt: 1}, false
	}
	return hint, true
}
