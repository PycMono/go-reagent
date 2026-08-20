---
name: weather-assistance
description: >-
  Retrieve real weather conditions or forecasts and turn them into practical
  advice. Use when the answer depends on weather, temperature, precipitation,
  wind, clothing, umbrellas, or outdoor plans. Triggers include "天气",
  "明天会下雨吗", "穿什么", "带伞吗", "weather", and "forecast". Do not use
  when the task is climate knowledge that needs no current data, or when no location and no
  weather-dependent question are present.
---

# 天气协助

## 目标

用真实天气数据回答当前状况或预报问题，并给出与数据一致的出行、穿衣或活动建议。

## 必要输入

- 地点；
- 用户关心的日期或时间段；
- 若要给活动建议，需知道活动类型和大致时段。

上下文已有地点或日期时直接复用。仅缺地点时只询问地点。

## 硬门禁

- 实时天气或预报必须调用 `web_search_exa`，不用模型记忆猜测。
- 搜索摘要不足以支持结论时，必须对选中的可信来源调用 `web_fetch_exa`。
- Exa 失败、来源过旧或可信来源冲突时，明确说明无法确认；不回退到其他公网数据源。

## 执行流程

1. 解析地点和相对日期；相对日期必须结合当前会话日期转换为明确日期。
2. 使用“地点 + 明确日期 + 用户关心的天气指标”调用 `web_search_exa`。
3. 优先选择气象机构、政府部门或可信天气服务；摘要不足时调用 `web_fetch_exa`。
4. 多个可信来源冲突时分别说明，不拼接成一份虚构预报。
5. 先给天气结论，再给与证据一致的穿衣、出行或活动建议，并注明来源和查询时间。

## 输出契约

遵守根 AGENTS.md 的语言与纯文本规则。明确写出地点和日期，随后用自然段给出温度、降水、风等与问题相关且有来源支持的数据，再用普通文本给出一到三条实用建议，并注明来源和查询时间。用户没有明确要求结构化格式时，不添加 Markdown 标题、加粗或列表标记。

## References 与 Templates

本 Skill 不依赖额外文件。使用 `web_search_exa` 发现当前天气来源，必要时使用 `web_fetch_exa` 核对原文。

## 边界

气候原理、季节常识等不需要当前数据的问题可以直接回答。灾害预警、空气质量和远期预报只有在 Exa 找到可信的当前来源时才能确认；没有充分证据时明确限制。

## 示例

用户问“杭州明天带伞吗”，应将“明天”转换为明确日期，通过 Exa 查询杭州该日预报，优先采用气象机构或可信天气服务，并依据有来源支持的降水信息回答；不要只说“南方天气多变，建议带伞”。

## 常见错误

- 没调用 Exa 就给出具体实时温度或预报；
- 只看搜索摘要不足以支撑结论，却不核对原文；
- 把过期页面或历史气候当成指定日期预报；
- 混合冲突来源生成看似完整的数据；
- 给穿衣建议却不说明对应温度、风雨条件、来源或查询时间。
