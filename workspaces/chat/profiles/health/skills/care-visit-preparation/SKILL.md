---
name: care-visit-preparation
description: >-
  Prepare a concise appointment brief, medication and history checklist,
  records list, and prioritized questions for a healthcare visit. Use when the
  user is going to a clinic, hospital, telehealth visit, or follow-up and wants
  to communicate efficiently. Triggers include "看医生前准备", "要带什么资料",
  "怎么跟医生说", "复诊问题清单", "prepare for appointment", and "doctor
  questions". Do not use when the task is deciding that care is unnecessary, diagnosing a
  symptom, or merely explaining one report item.
---

# 就医准备

## 目标

帮助用户在有限就诊时间内清楚说明主要问题、变化和背景，并准备真实资料与优先问题。

## 必要输入

- 就诊科室、目的、时间和本次最想解决的问题；
- 症状或检查的简明时间线；
- 既往情况、过敏、用药和已有资料；
- 医疗机构已给出的准备要求。

## 硬门禁

- 先读取 `profiles/health/references/health-safety-boundaries.md` 和 `profiles/health/references/health-information-fields.md`。
- 不根据在线整理判断用户“不需要去医院”或指定诊疗方案。
- 出现相关紧急信号时，优先建议立即获得当地医疗帮助，不等待预约。
- 不建议为了检查自行停药、禁食或改变治疗，除非用户提供医疗机构的明确指示。

## 执行流程

1. 明确本次就诊目标和最影响生活的问题。
2. 将症状、检查和处理按时间压缩成一段可口述摘要。
3. 整理药物、过敏、既往情况及相关报告，只保留与本次目标有关的材料。
4. 按重要性准备三到五个问题，优先诊断思路、检查目的、下一步和何时求助。
5. 检查交通、陪同、语言、行动或记录等现实支持需求。

## 输出契约

输出“一分钟摘要、携带资料、用药/过敏清单、优先问题、现场记录项、需要提前求助的条件”。

## References 与 Templates

安全边界读取 `profiles/health/references/health-safety-boundaries.md`，字段清单读取 `profiles/health/references/health-information-fields.md`。需要完整简报时读取 `profiles/health/skills/care-visit-preparation/templates/visit-brief.md`。

## 边界

本 Skill 优化沟通，不替代分诊、诊断或医生安排。症状时间线使用 `symptom-organizing`，报告逐项解释使用 `health-report-explanation`。

## 示例

首次去心内科时，应整理主要不适时间线、相关报告、药物过敏和最重要的问题；不要建议用户自行停用影响检查的药物。

## 常见错误

- 材料清单过长，没有就诊重点；
- 问题全部是“我得了什么”，没有下一步和风险条件；
- 擅自给检查前停药或禁食指示；
- 忽略紧急症状而等待预约。
