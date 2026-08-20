---
name: symptom-organizing
description: >-
  Organize a user's symptoms into a clear timeline, identify missing observation
  fields, and prepare a neutral summary without diagnosing. Use when the user
  describes discomfort, changes, or recurring symptoms and wants to know what
  to record or communicate. Triggers include "症状怎么整理", "最近一直头晕",
  "需要记录什么", "symptom timeline", and "what should I track". Do not use
  when the task is explaining a lab or imaging report, preparing a complete appointment
  checklist, or giving a diagnosis or prescription.
---

# 症状整理

## 目标

把零散的身体感受整理成准确、简短、可供用户观察或就医沟通的时间线，并识别真正重要的信息缺口。

## 必要输入

按当前问题从症状、开始时间、变化、严重程度、伴随表现、影响、相关病史和用药中选择必要字段，不机械询问全部内容。

## 硬门禁

- 先读取 `profiles/health/references/health-safety-boundaries.md`；涉及字段补全时再读取 `profiles/health/references/health-information-fields.md`。
- 不根据整理结果给出确定诊断、处方或药物剂量。
- 发现相关紧急信号时，先建议联系当地急救或立即获得线下医疗帮助，不继续冗长采集。
- 不建议用户自行停药、换药或调整处方药。

## 执行流程

1. 先判断是否存在与当前描述相关的紧急信号。
2. 用用户原话确定主要不适，按时间建立开始、变化和当前状态。
3. 补充少量会改变风险理解或沟通质量的字段，一次优先问最重要的问题。
4. 区分已知事实、用户感受、未确认关联和需要专业判断的问题。
5. 生成中性摘要，并给出后续观察点或就医时可说明的信息。

## 输出契约

输出“当前摘要、时间线、相关表现/影响、已知背景、待补信息、需要及时求助的条件”。不得输出最可能诊断排行榜。

## References 与 Templates

安全判断读取 `profiles/health/references/health-safety-boundaries.md`，字段选择读取 `profiles/health/references/health-information-fields.md`。需要结构化摘要时读取 `profiles/health/skills/symptom-organizing/templates/symptom-summary.md`。

## 边界

本 Skill 只组织信息，不判断用户是否无需就医。报告指标解释使用 `health-report-explanation`，完整就医准备使用 `care-visit-preparation`。

## 示例

用户说“头晕三天”时，应整理首次出现、每次持续、诱因、伴随表现和功能影响；不要直接回答“这是低血糖”。

## 常见错误

- 按完整问诊清单连续追问；
- 把“可能相关”写成因果结论；
- 遗漏时间变化和严重程度；
- 紧急信号出现后仍先完成模板。
