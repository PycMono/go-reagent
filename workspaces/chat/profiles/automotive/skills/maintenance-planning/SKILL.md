---
name: maintenance-planning
description: >-
  Build a maintenance plan from the exact vehicle manual, age, mileage,
  dashboard reminders, service history, and severe-use conditions. Use when the
  user wants to know what is due, how to organize records, or what to ask at a
  service visit. Triggers include "保养计划", "多少公里保养", "保养手册怎么看",
  "哪些项目到期", "maintenance schedule", and "service interval". Do not use
  when the task is diagnosing a current abnormal symptom, comparing vehicles, or inventing
  exact intervals or fluid specifications without model-specific evidence.
---

# 保养规划

## 目标

以对应车辆手册和真实记录为依据，区分已到期、即将到期、按状态检查和待核实项目，避免过度或遗漏保养。

## 必要输入

- 地区、车型、年款、动力形式、里程和车龄；
- 用户手册/保养手册、车辆提示和历史记录；
- 年里程、短途、拥堵、极端温度、粉尘、拖挂等使用条件；
- 当前是否存在异常现象。

## 硬门禁

- 先读取 `profiles/automotive/references/vehicle-information-framework.md`；涉及安全系统时读取 `profiles/automotive/references/vehicle-safety-boundaries.md`。
- 不凭通用经验编造精确周期、油液规格、零件型号或“终身免维护”结论。
- 不把异常故障当作常规保养项目处理。
- 不建议绕过车辆告警、厂商安全要求或环保要求。

## 执行流程

1. 核对车型资料、地区、手册版本和时间/里程双条件。
2. 对照历史记录确定已完成、即将到期、已超期和记录缺失项目。
3. 根据手册定义判断是否符合特殊/严苛使用条件，不自行扩大范围。
4. 将项目分为更换、检查、按状态处理和待专业确认。
5. 生成时间线、资料缺口和到店问题，保留票据与实际完成记录。

## 输出契约

输出“依据与假设、当前状态、近期项目、中期项目、按状态检查项、待核实资料、到店问题”。每个精确周期注明来源。

## References 与 Templates

规划字段读取 `profiles/automotive/references/vehicle-information-framework.md`，安全边界读取 `profiles/automotive/references/vehicle-safety-boundaries.md`。需要计划表时读取 `profiles/automotive/skills/maintenance-planning/templates/maintenance-plan.md`。

## 边界

本 Skill 不替代车辆手册和专业检查。当前异常使用 `vehicle-symptom-triage`，购车比较使用 `vehicle-comparison`。

## 示例

低年里程车辆也应同时核对时间条件、车辆提示和使用环境；不能只按公里数断言所有项目都无需处理。

## 常见错误

- 不确认年款和动力形式就给周期；
- 把所有门店建议都当成厂商要求；
- 忽略时间条件和特殊使用条件；
- 用保养计划掩盖当前故障症状。
