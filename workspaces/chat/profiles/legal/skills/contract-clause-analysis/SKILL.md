---
name: contract-clause-analysis
description: >-
  Analyze user-provided contract language by identifying obligations, rights,
  triggers, timing, dependencies, ambiguity, and practical risk questions. Use
  when the user asks what a clause means or wants clauses compared or reviewed.
  Triggers include "分析合同条款", "自动续约风险", "违约责任怎么看",
  "这条是什么意思", "review this clause", and "contract risk". Do not use when the task is a pure
  event timeline, a full consultation checklist, or a definitive enforceability
  opinion without jurisdiction-specific professional review.
---

# 合同条款分析

## 目标

把合同文字拆成可执行的权利、义务、条件和后果，指出歧义、缺失机制及需要谈判或专业复核的问题。

## 必要输入

- 司法辖区、合同类型和当事人身份；
- 条款原文、相关定义、附件和交叉引用；
- 用户目标、当前阶段以及是否已签署或履行。

## 硬门禁

- 先读取 `profiles/legal/references/legal-information-boundaries.md` 和 `profiles/legal/references/legal-review-framework.md`。
- 不孤立解释缺少定义、上下文或交叉引用的条款。
- 不承诺条款有效/无效、必然胜诉或给出未经核实的具体法条。
- 签署、终止、付款、承认责任或放弃权利前，提示及时获得当地专业复核。

## 执行流程

1. 确认辖区、合同阶段和用户最关心的实际结果。
2. 保留原文，识别主体、动作、触发条件、时间、通知和证明要求。
3. 追踪定义、附件和其他条款，检查权利义务是否对称及机制是否完整。
4. 区分明确效果、合理疑问和必须结合当地规则判断的事项。
5. 形成风险表及可向对方或律师确认的具体问题，不直接代拟欺骗性规避方案。

## 输出契约

按条款输出“通俗含义、谁在何时做什么、触发/例外、可能影响、歧义/缺口、待确认问题”。引用原文时保持准确。

## References 与 Templates

边界读取 `profiles/legal/references/legal-information-boundaries.md`，审阅维度读取 `profiles/legal/references/legal-review-framework.md`。需要风险表时读取 `profiles/legal/skills/contract-clause-analysis/templates/clause-risk-table.md`。

## 边界

本 Skill 提供一般信息和审阅框架，不替代当地律师对完整合同及适用法律的意见。争议事实整理使用 `facts-and-evidence-organizing`。

## 示例

分析自动续约条款时，应找续约周期、通知窗口、方式、费用变化和终止后果；不要只回答“自动续约有风险”。

## 常见错误

- 只看单句，不看定义和关联条款；
- 把不利商业条件直接等同于违法；
- 忽略通知方式和时间条件；
- 用确定语气替代辖区核实。
