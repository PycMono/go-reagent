---
name: legal-consultation-preparation
description: >-
  Prepare a concise matter brief, priority questions, records checklist, goals,
  and constraints for a consultation with a lawyer or other legal professional.
  Use when the user has or plans a professional consultation and wants to use
  it efficiently. Triggers include "咨询律师前准备", "要带哪些材料",
  "问律师什么", "案件简报", "prepare for a lawyer", and "legal consultation".
  Do not use when the task is predicting case outcome, replacing formal legal advice, or only
  analyzing one clause or organizing one timeline.
---

# 法律咨询准备

## 目标

把用户的事项压缩成专业人士能快速理解的事实简报、材料清单和优先问题，同时暴露紧迫日期与现实约束。

## 必要输入

- 辖区、事项类型、当前程序阶段和咨询时间；
- 关键主体、时间线、材料和已采取行动；
- 用户目标、可接受方案、预算或时间限制；
- 正式文件、截止日期或不可逆决定。

## 硬门禁

- 先读取 `profiles/legal/references/legal-information-boundaries.md` 和 `profiles/legal/references/legal-review-framework.md`。
- 不替专业人士给出最终策略、胜诉概率或承诺结果。
- 不建议删改、隐匿、补造材料。
- 存在正式文书、临近日期或重大不可逆决定时，优先提示尽快联系当地专业人士。

## 执行流程

1. 用一句话说明事项、辖区、当前阶段和用户目标。
2. 从材料中提取最关键时间线、争议点和对方主张。
3. 按重要性整理原始材料，标记缺失和未核实事项。
4. 准备三到七个优先问题，覆盖适用规则、选项、风险、证据和下一步。
5. 明确咨询后需要记录的决定、截止时间、责任主体和补充材料。

## 输出契约

输出“事项摘要、关键时间线、材料清单、当前风险/日期、用户目标与限制、优先问题、咨询后记录项”。

## References 与 Templates

边界读取 `profiles/legal/references/legal-information-boundaries.md`，整理框架读取 `profiles/legal/references/legal-review-framework.md`。需要完整简报时读取 `profiles/legal/skills/legal-consultation-preparation/templates/consultation-brief.md`。

## 边界

本 Skill 准备咨询，不替代咨询。详细证据索引使用 `facts-and-evidence-organizing`，条款逐项解释使用 `contract-clause-analysis`。

## 示例

劳动争议咨询应准备辖区、劳动关系、关键日期、合同/工资/沟通材料、已收到文件和目标问题；不要只整理一份情绪化叙述。

## 常见错误

- 材料很多但没有优先顺序；
- 漏掉正式文件和临近日期；
- 问题只剩“能不能赢”；
- 把未经证实的推测写入事实摘要。
