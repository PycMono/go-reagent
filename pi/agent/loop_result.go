package agent

import "github.com/PycMono/go-reagent/pi/ai"

type loopResult struct {
	newMessages []ai.Message
	invocations []ModelInvocation
}
