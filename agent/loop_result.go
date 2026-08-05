package agent

import "github.com/PycMono/go-reagent/ai"

type loopResult struct {
	newMessages []ai.Message
	invocations []ModelInvocation
}
