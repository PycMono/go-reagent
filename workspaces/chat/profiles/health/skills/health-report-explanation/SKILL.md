---
name: health-report-explanation
description: >-
  Explain user-provided laboratory, screening, examination, or imaging-report
  wording in plain language while preserving units, ranges, and uncertainty.
  Use when the user wants to understand a test item, abnormal flag, report
  phrase, or trend. Triggers include "体检报告怎么看", "指标偏高", "化验单",
  "影像报告什么意思", "lab result", and "report explanation". Do not use when the task is
  diagnosing from a result, organizing symptoms alone, or planning the whole
  clinical visit.
---

# 健康报告解释

## 目标

帮助用户看懂报告项目、异常标记和可向医生确认的问题，同时保留检查本身的上下文与不确定性。

## 必要输入

- 项目或报告原文；
- 数值、单位、参考范围、异常标记和日期；
- 检查原因、相关症状、历史结果及医生已有说明。

提醒用户按需遮盖可识别信息。关键数值缺单位或参考范围时，不做精确判断。

## 硬门禁

- 先读取 `profiles/health/references/health-safety-boundaries.md`；需要字段核对时读取 `profiles/health/references/health-information-fields.md`。
- 不把单项异常等同于疾病，不根据报告确诊、分期或开药。
- 不篡改用户提供的数值、单位、范围或报告措辞。
- 不建议自行停药、换药或用补充剂“纠正指标”。

## 执行流程

1. 核对项目名称、结果、单位、参考范围、日期和检查背景。
2. 用通俗语言解释该项目通常反映什么以及报告用语的含义。
3. 说明异常可能受哪些类别因素影响，但不列无边界疾病清单。
4. 结合历史结果区分单次异常与趋势；资料不足时明确缺口。
5. 生成需要与医生确认的少量问题和可能需要携带的资料。

## 输出契约

逐项输出“原始结果、通俗含义、上下文/限制、建议确认的问题”。最后给整体摘要，但不生成诊断结论。

## References 与 Templates

解释边界读取 `profiles/health/references/health-safety-boundaries.md`，字段核对读取 `profiles/health/references/health-information-fields.md`。多项目报告可读取 `profiles/health/skills/health-report-explanation/templates/report-questions.md`。

## 边界

无法查看原始影像或完成体格检查时必须明确限制。症状整理使用 `symptom-organizing`，完整就医准备使用 `care-visit-preparation`。

## 示例

用户说“转氨酶偏高”但没有数值、单位和范围时，应先说明项目一般含义并请求关键字段，不直接判断严重程度或病因。

## 常见错误

- 只看到 H/L 标记就下结论；
- 忽略单位和实验室参考范围；
- 罗列大量疾病制造焦虑；
- 用正常单项结果排除全部风险。
