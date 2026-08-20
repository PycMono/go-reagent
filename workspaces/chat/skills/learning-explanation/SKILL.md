---
name: learning-explanation
description: >-
  Explain an unfamiliar idea clearly with an appropriate example and level of
  detail. Use when the user asks for a brief explanation or says they do not
  understand something. Triggers include "解释一下", "什么意思", "没看懂",
  "举个例子", "explain", and "help me understand". Do not use when the task is a simple
  factual lookup, for creating exercises or study schedules, or when the
  Learning Profile's more specific concept-explanation Skill is available.
---

# 学习讲解

## 目标

让用户快速建立一个可用的理解，而不是堆积术语或把简单问题扩展成完整课程。

## 必要输入

- 需要解释的概念、句子或现象；
- 可从表达中判断的知识水平和使用目的。

只有解释深度会明显受影响且无法推断时，才询问用户背景。

## 硬门禁

- 不伪造教材出处、研究结论或引用。
- 对存在争议或多种定义的概念，不把单一解释包装成唯一结论。
- 不在用户只要一个事实答案时强制进入教学流程。

## 执行流程

1. 先用一两句话给出核心含义。
2. 拆出最少数量的关键组成或因果关系。
3. 给一个与用户场景贴近的例子，并指出例子对应关系。
4. 必要时补充一个容易混淆的反例或边界。
5. 用户表示没理解时，更换类比、表示方式或切入点，不重复原文。

## 输出契约

默认按“核心解释 → 例子 → 必要边界”输出。简单概念控制在短段落内；除非用户要求，不自动附测验或学习计划。

## References 与 Templates

本 Skill 不依赖额外文件。需要系统教学、练习或计划时，使用 Learning Profile 的专用 Skill。

## 边界

本 Skill 提供通用、短时讲解，不负责考试政策、实时课程信息、练习设计或长期学习管理。

## 示例

解释“复利”时先说明“收益也继续产生收益”，再给小额、少周期的数字例子；无需先讲完整金融史。

## 常见错误

- 先给术语定义，再用更多术语解释；
- 例子与概念结构不对应；
- 简单问题回答过长；
- 用户没理解时只换一种措辞重复同一段话。
