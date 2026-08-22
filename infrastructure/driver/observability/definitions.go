package observability

import (
	"fmt"

	sdkmetrics "github.com/PycMono/go-observability-sdk/metrics"
	piobservability "github.com/PycMono/go-reagent/pi/harness/observability"
)

// DomainDefinitions 把 pi 语义层固定的领域指标定义（设计 §8）转换为
// go-observability-sdk 的 metrics.Definition，供 Runtime 注册并生成显式
// Bucket View。P0/P1 一并注册：P1 的语义先固定，instrument 记录从阶段 5 开始。
func DomainDefinitions() ([]sdkmetrics.Definition, error) {
	domain := piobservability.DomainMetricDefinitions()
	definitions := make([]sdkmetrics.Definition, 0, len(domain))
	for _, item := range domain {
		definition, err := convertDefinition(item)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func convertDefinition(item piobservability.MetricDefinition) (sdkmetrics.Definition, error) {
	var kind sdkmetrics.Kind
	switch item.Kind {
	case piobservability.MetricKindCounter:
		kind = sdkmetrics.KindCounter
	case piobservability.MetricKindHistogram:
		kind = sdkmetrics.KindHistogram
	case piobservability.MetricKindTimer:
		kind = sdkmetrics.KindTimer
	default:
		return sdkmetrics.Definition{}, fmt.Errorf("observability: unknown domain metric kind %q for %q", item.Kind, item.Name)
	}
	return sdkmetrics.Definition{
		Name:        item.Name,
		Kind:        kind,
		Description: item.Description,
		Unit:        item.Unit,
		Labels:      item.Labels,
		Buckets:     item.Buckets,
	}, nil
}

// ForbiddenLabelKeys 返回设计 §8.5 的 Label 基数红线，作为 Runtime 的
// 禁止 Label Key 追加集合。
func ForbiddenLabelKeys() []string {
	return piobservability.ForbiddenLabelKeys
}
