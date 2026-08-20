---
name: vehicle-symptom-triage
description: >-
  Organize a vehicle abnormality, identify immediate safety concerns, collect
  reproducible conditions, and prepare a repair intake summary without remote
  diagnosis. Use when the user reports a warning, noise, vibration, smell,
  leak, handling change, or intermittent fault. Triggers include "车辆异响",
  "故障灯亮了", "刹车抖动", "还能不能开", "car symptom", and "warning light".
  Do not use when the task is comparing cars, routine maintenance scheduling, or unsafe
  repair instructions.
---

# 车辆异常分级与送修准备

## 目标

先识别是否可能影响安全，再把异常现象、触发条件和原始信息整理成可复现、便于专业检查的送修描述。

## 必要输入

- 车型、年款、动力形式、里程和近期维修；
- 告警原文及现象的位置、性质、首次时间和趋势；
- 车速、路况、冷热车、制动、转向或加速等触发条件；
- 是否影响制动、转向、动力、视野或稳定性。

## 硬门禁

- 必须先读取 `profiles/automotive/references/vehicle-safety-boundaries.md`，再按需读取 `profiles/automotive/references/vehicle-information-framework.md`。
- 不远程保证车辆可以继续驾驶，不把可能原因写成确定故障。
- 不指导用户拆修制动、燃油、高压、气囊或举升相关系统。
- 不要求先清除故障码、告警或原始记录。

## 执行流程

1. 先判断是否涉及停止驾驶、远离风险区域或联系道路救援的条件。
2. 记录车辆身份、告警原文、异常类型、位置和当前状态。
3. 建立首次出现、频率、变化趋势和可复现条件。
4. 区分观察到的事实、用户猜测和专业检查才能确认的原因。
5. 生成送修摘要、可安全记录的材料和需要维修方回答的问题。

## 输出契约

输出“安全优先级、现象摘要、触发条件、近期变化/维修、保留信息、送修问题”。可能原因只按系统类别说明，不给确定诊断。

## References 与 Templates

安全判断读取 `profiles/automotive/references/vehicle-safety-boundaries.md`，信息字段读取 `profiles/automotive/references/vehicle-information-framework.md`。需要送修摘要时读取 `profiles/automotive/skills/vehicle-symptom-triage/templates/repair-intake.md`。

## 边界

远程整理不能替代诊断仪、举升检查或实车路试。比较车型使用 `vehicle-comparison`，周期性项目使用 `maintenance-planning`。

## 示例

高速制动方向盘抖动时，应先确认制动能力和车辆稳定是否异常，再记录速度、制动力度、频率和近期维修；不要直接指导拆换制动盘。

## 常见错误

- 根据一个声音名称直接判定零件；
- 未检查安全影响就讨论维修价格；
- 建议清码后继续观察；
- 给出缺乏设备条件的拆修步骤。
