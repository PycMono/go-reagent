---
name: facts-and-evidence-organizing
description: >-
  Structure a legal or dispute-related narrative into parties, a dated
  timeline, claims, supporting materials, contradictions, and evidence gaps.
  Use when the user has events, messages, payments, notices, or documents that
  need neutral organization. Triggers include "整理案件时间线", "证据清单",
  "事实经过", "材料怎么整理", "case timeline", and "organize evidence".
  Do not use when the task is analyzing contract language itself, preparing the complete
  lawyer consultation agenda, or predicting who will win.
---

# 事实与证据整理

## 目标

把争议叙述转化为主体明确、日期可核对、事实与主张分开的时间线和材料索引，暴露真正的信息缺口。

## 必要输入

- 司法辖区和事项类型；
- 参与主体、关键日期、事件和用户目标；
- 合同、通知、聊天、付款等材料及其来源。

## 硬门禁

- 先读取 `profiles/legal/references/legal-information-boundaries.md` 和 `profiles/legal/references/legal-review-framework.md`。
- 不伪造、补全、修改或指导销毁证据。
- 不把用户判断、对方说法或推测写成已证实事实。
- 不根据材料数量预测胜诉或给出确定法律结论。

## 执行流程

1. 确认辖区、当前程序阶段和是否存在临近日期或正式文书。
2. 列出主体及身份，按日期整理事件、行为人、来源和后续影响。
3. 将每项内容标记为已确认、单方陈述、争议或待核实。
4. 建立证据与事实的对应关系，保留原始文件和完整上下文。
5. 汇总矛盾、缺口、待核实事项和需要专业人士判断的问题。

## 输出契约

输出“事项摘要、主体、时间线、证据索引、争议/矛盾、证据缺口、紧迫事项”。不得输出胜负结论。

## References 与 Templates

边界读取 `profiles/legal/references/legal-information-boundaries.md`，整理字段读取 `profiles/legal/references/legal-review-framework.md`。需要结构化交付时读取 `profiles/legal/skills/facts-and-evidence-organizing/templates/case-timeline.md`。

## 边界

本 Skill 组织信息，不鉴定证据真伪或可采性。合同文字分析使用 `contract-clause-analysis`，咨询议程使用 `legal-consultation-preparation`。

## 示例

押金争议应按签约、付款、交付、退租、验收和催告排序，并分别链接合同、转账和聊天；不要把“房东故意拖欠”写成事实。

## 常见错误

- 按情绪重要性而非时间排序；
- 截取聊天而丢失上下文；
- 混淆事实、主张和法律结论；
- 为填补空白推测日期或行为。
