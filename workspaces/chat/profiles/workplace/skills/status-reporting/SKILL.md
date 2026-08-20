---
name: status-reporting
description: >-
  Turn verified work facts into a project update, weekly report, progress
  summary, meeting brief, or management status view. Use when the user needs to
  report accomplishments, progress, risks, decisions, or next steps. Triggers
  include "周报", "项目进展", "汇报材料", "复盘进度", "status report", and
  "project update". Do not use when the task is one short message or for planning a
  difficult interpersonal conversation.
---

# 工作汇报

## 目标

把真实工作信息组织成便于决策和跟进的汇报，清楚区分完成情况、风险、依赖和下一步。

## 必要输入

- 汇报对象、周期和期望用途；
- 已完成、进行中、数据结果、风险、依赖和下一步；
- 需要决策或支持的事项。

## 硬门禁

- 不虚构成果、数字、完成比例、客户反馈或审批状态。
- 不把计划写成已完成，不隐藏对目标有实质影响的风险。
- 缺失数据使用“待确认”或占位符，不用模糊形容词替代。

## 执行流程

1. 按汇报对象确定决策粒度，提取本周期最重要的变化。
2. 将信息分成结果、进展、风险与依赖、下一步和待决策事项。
3. 对数字保留口径、周期和来源；对风险写清影响与应对状态。
4. 让每个下一步都有责任主体、预期时间或明确待确认项。
5. 删除流水账和与汇报目标无关的过程细节。

## 输出契约

默认输出摘要、关键进展、风险/依赖、下一步和需支持事项。内容必须能区分事实状态与计划状态。

## References 与 Templates

需要标准汇报结构时，读取 `profiles/workplace/skills/status-reporting/templates/status-report.md` 并按受众删减字段。

## 边界

本 Skill 组织用户提供的事实，不负责生成不存在的业绩或验证企业内部数据。单条通知使用 `work-message-writing`。

## 示例

“接口已完成，联调受第三方沙箱影响”应拆为已完成事项、当前依赖、影响、应对动作和需要的支持，而不是合并成“整体进展顺利”。

## 常见错误

- 工作过程很多，但没有结果和影响；
- 计划与完成状态混写；
- 风险只有名称，没有影响和动作；
- 为显得积极而删除关键不确定性。
