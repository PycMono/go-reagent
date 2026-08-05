package observability

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/ai"
)

type clientFunc func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error)

func (f clientFunc) Generate(ctx context.Context, messages []ai.Message, tools []ai.ToolDefinition) (*ai.Message, error) {
	return f(ctx, messages, tools)
}

func TestCostTrackerCalculatesEverySuccessfulCall(t *testing.T) {
	original := &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{
		InputTokens: 2_000_000, OutputTokens: 500_000,
	}}
	times := []time.Time{time.Unix(0, 0), time.Unix(0, 0).Add(2500 * time.Millisecond)}
	index := 0
	tracker, err := newCostTracker(
		clientFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return original, nil
		}),
		"zhipu", "glm-4.5-air",
		Pricing{InputUSDPerMillionTokens: 0.15, OutputUSDPerMillionTokens: 0.60},
		func() time.Time {
			value := times[index]
			index++
			return value
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tracker.Generate(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage.CostUSD != 0.60 || result.Usage.LatencyMS != 2500 ||
		result.Usage.PlatformID != "zhipu" || result.Usage.Model != "glm-4.5-air" {
		t.Fatalf("Usage = %#v", result.Usage)
	}
	if original.Usage.CostUSD != 0 || original.Usage.LatencyMS != 0 || original.Usage.PlatformID != "" {
		t.Fatalf("delegate Usage mutated: %#v", original.Usage)
	}
}

func TestCostTrackerRejectsMissingUsage(t *testing.T) {
	tracker, err := NewCostTracker(
		clientFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("uncosted")}}, nil
		}),
		"test", "model", Pricing{},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tracker.Generate(context.Background(), nil, nil)
	if err == nil || !errors.Is(err, ai.ErrGeneration) {
		t.Fatalf("Generate() error = %v, want generation error", err)
	}
	if result != nil {
		t.Fatalf("Generate() result = %#v, want nil on missing Usage", result)
	}
}

func TestCostTrackerRejectsInvalidTokenUsage(t *testing.T) {
	for _, usage := range []*ai.Usage{{InputTokens: -1}, {OutputTokens: -1}} {
		tracker, err := NewCostTracker(
			clientFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
				return &ai.Message{Role: ai.RoleAssistant, Usage: usage}, nil
			}),
			"test", "model", Pricing{},
		)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := tracker.Generate(context.Background(), nil, nil); result != nil || !errors.Is(err, ai.ErrGeneration) {
			t.Fatalf("Generate() = %#v, %v, want nil generation error", result, err)
		}
	}
}

func TestCostTrackerAllowsFreePricing(t *testing.T) {
	tracker, err := NewCostTracker(
		clientFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{InputTokens: 3, OutputTokens: 4}}, nil
		}),
		"test", "free-model", Pricing{},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tracker.Generate(context.Background(), nil, nil)
	if err != nil || result.Usage.CostUSD != 0 {
		t.Fatalf("Generate() = %#v, %v", result, err)
	}
}

func TestCostTrackerPreservesDelegateError(t *testing.T) {
	want := errors.New("provider failed")
	response := &ai.Message{Role: ai.RoleAssistant}
	tracker, err := NewCostTracker(
		clientFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return response, want
		}),
		"test", "model", Pricing{},
	)
	if err != nil {
		t.Fatal(err)
	}
	gotResponse, gotErr := tracker.Generate(context.Background(), nil, nil)
	if gotResponse != response || !errors.Is(gotErr, want) {
		t.Fatalf("Generate() = %#v, %v, want original response/error", gotResponse, gotErr)
	}
}

func TestCostTrackerRejectsInvalidConstruction(t *testing.T) {
	validClient := clientFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
		return nil, nil
	})
	tests := []struct {
		name       string
		next       ai.Client
		platformID string
		model      string
		pricing    Pricing
	}{
		{name: "nil client", platformID: "p", model: "m"},
		{name: "blank platform", next: validClient, platformID: " ", model: "m"},
		{name: "blank model", next: validClient, platformID: "p", model: " "},
		{name: "negative input price", next: validClient, platformID: "p", model: "m", pricing: Pricing{InputUSDPerMillionTokens: -1}},
		{name: "NaN input price", next: validClient, platformID: "p", model: "m", pricing: Pricing{InputUSDPerMillionTokens: math.NaN()}},
		{name: "infinite output price", next: validClient, platformID: "p", model: "m", pricing: Pricing{OutputUSDPerMillionTokens: math.Inf(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewCostTracker(tt.next, tt.platformID, tt.model, tt.pricing); err == nil {
				t.Fatal("NewCostTracker() error = nil")
			}
		})
	}
}

func TestCostTrackerKeepsConcurrentCallsIndependent(t *testing.T) {
	shared := &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{InputTokens: 1_000_000}}
	tracker, err := NewCostTracker(
		clientFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return shared, nil
		}),
		"test", "model", Pricing{InputUSDPerMillionTokens: 0.25},
	)
	if err != nil {
		t.Fatal(err)
	}

	const callCount = 32
	results := make(chan *ai.Message, callCount)
	errorsCh := make(chan error, callCount)
	for range callCount {
		go func() {
			result, err := tracker.Generate(context.Background(), nil, nil)
			results <- result
			errorsCh <- err
		}()
	}
	seen := make(map[*ai.Usage]struct{}, callCount)
	for range callCount {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
		result := <-results
		if result == nil || result.Usage == nil || result.Usage.CostUSD != 0.25 {
			t.Fatalf("result = %#v", result)
		}
		seen[result.Usage] = struct{}{}
	}
	if len(seen) != callCount {
		t.Fatalf("unique Usage pointers = %d, want %d", len(seen), callCount)
	}
	if shared.Usage.PlatformID != "" || shared.Usage.CostUSD != 0 {
		t.Fatalf("shared delegate response mutated: %#v", shared)
	}
}

func TestCostTrackerErrorsDoNotLeakContent(t *testing.T) {
	tracker, err := NewCostTracker(
		clientFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("private output")}}, nil
		}),
		"test", "model", Pricing{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tracker.Generate(context.Background(), nil, nil)
	if err == nil || strings.Contains(err.Error(), "private output") {
		t.Fatalf("Generate() error = %v", err)
	}
}
