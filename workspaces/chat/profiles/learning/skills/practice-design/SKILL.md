---
name: practice-design
description: >-
  Create targeted exercises, quizzes, worked examples, answer keys, or feedback
  that diagnose and strengthen a specific skill. Use when the user asks to
  practise, test understanding, review an answer, or increase difficulty.
  Triggers include "出几道题", "练习一下", "测测我", "批改答案",
  "quiz me", and "practice problems". Do not use when the user only needs a
  concept explanation or a calendar-based study plan.
---

# 练习设计与反馈

## 目标

围绕明确能力点设计有梯度、可作答、可反馈的练习，让错误暴露具体理解缺口。

## 必要输入

- 学习主题、能力目标和当前水平；
- 题量、题型、难度、是否立即提供答案；
- 用户已有答案时，保留其原始作答和思路。

## 硬门禁

- 不声称题目来自某次真实考试，除非用户提供可靠来源。
- 不把有歧义、信息不足或超出已学范围的题目当作能力判断依据。
- 不代替用户参加考试或规避学术诚信要求。

## 执行流程

1. 将目标拆成可观察能力点，例如识别、计算、解释或迁移。
2. 设计由基础到综合的题目，每题对应一个主要诊断目标。
3. 检查题干信息充分、答案唯一性或开放题评分标准。
4. 按用户要求决定答案与提示的展示时机。
5. 批改时先指出正确部分，再定位错误步骤、原因和下一道针对性练习。

## 输出契约

题目区与答案区明确分开。答案包含关键步骤或评分点，而不是只给最终结果；反馈包含“表现、具体误区、下一步练习”。

## References 与 Templates

本 Skill 不依赖额外文件。长期练习节奏应交给 `study-planning` 安排。

## 边界

练习结果只能反映当前题目表现，不能据此给用户贴能力标签或作心理、医学诊断。

## 示例

用户练一元二次方程时，可从辨认形式、直接求解到应用题逐步增加负荷，并让错误选项对应常见符号或判别式误区。

## 常见错误

- 题目数量多但能力点重复；
- 难度突然跳跃；
- 先展示答案导致无法练习；
- 批改只说“错了”，没有定位步骤。
