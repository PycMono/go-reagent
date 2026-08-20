---
name: vehicle-comparison
description: >-
  Compare candidate vehicles against the user's budget, passengers, roads,
  mileage, charging or parking conditions, and priorities using traceable data.
  Use when the user is choosing, shortlisting, or test-driving vehicles.
  Triggers include "两款车怎么选", "选车对比", "适合家用吗", "购车建议",
  "compare cars", and "which vehicle". Do not use when the task is diagnosing a vehicle
  symptom, planning maintenance, or claiming live price/configuration/recall
  facts without a current source.
---

# 车型比较

## 目标

围绕用户真实使用条件比较候选车辆，区分可验证参数、主观偏好和必须试驾/实车确认的项目。

## 必要输入

- 地区、预算口径和候选车型的准确年款/配置；
- 乘员、道路、年里程、单次距离、停车和补能条件；
- 用户核心优先级和不可接受项；
- 车型资料来源及日期。

## 硬门禁

- 先读取 `profiles/automotive/references/vehicle-information-framework.md`；涉及安全或召回时读取 `profiles/automotive/references/vehicle-safety-boundaries.md`。
- 不编造实时价格、在售配置、交付周期、优惠、召回或软件版本。
- 不把不同地区、年款或配置的数据混为同一车型事实。
- 不仅凭参数表保证安全、可靠性、舒适或保值表现。

## 执行流程

1. 确认使用场景、硬约束和排序最高的三项偏好。
2. 核对候选车型资料的地区、年款、配置、来源和未知项。
3. 选择与目标直接相关的比较维度，先排除违反硬约束的选项。
4. 分开呈现可验证数据、用户偏好、合理推断和需试驾确认项。
5. 给出条件化首选、备选、主要代价和购车前核对清单。

## 输出契约

输出“用户条件、比较表、条件化建议、主要代价、未知/实时待核实项、试驾与实车检查重点”。

## References 与 Templates

比较字段读取 `profiles/automotive/references/vehicle-information-framework.md`；安全和召回边界读取 `profiles/automotive/references/vehicle-safety-boundaries.md`。需要表格时读取 `profiles/automotive/skills/vehicle-comparison/templates/vehicle-comparison.md`。

## 边界

本 Skill 不替代试驾、第三方检测或合同审阅。车辆异常使用 `vehicle-symptom-triage`，保养安排使用 `maintenance-planning`。

## 示例

一家四口城市通勤和周末露营，应优先比较儿童座椅、行李、停车补能和高速舒适需求，而不是按最大功率直接选车。

## 常见错误

- 比较不同年款或配置却未标注；
- 维度很多但与用户场景无关；
- 把媒体体验写成用户必然感受；
- 用过期价格或配置作确定建议。
