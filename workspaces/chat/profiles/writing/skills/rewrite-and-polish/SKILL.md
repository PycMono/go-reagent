---
name: rewrite-and-polish
description: >-
  Transform an existing text while preserving its facts, intent, and required
  meaning. Use when the user wants rewriting, polishing, shortening, expansion,
  tone adjustment, or style alignment. Triggers include "润色", "改写",
  "精简", "扩写", "换个语气", "rewrite", and "polish". Do not use when the
  user needs social content designed from scratch or a long-form outline before
  drafting.
---

# 改写与润色

## 目标

在不扭曲事实、立场和承诺的前提下，让已有文本更符合用户指定的语气、长度和使用场景。

## 必要输入

- 原文；
- 期望用途、受众和调整方向；
- 必须保留的词句、事实、格式或长度上限。

## 硬门禁

- 不新增原文没有的人名、数字、因果、成绩、承诺或引用。
- 不因“更有说服力”而增强确定性、攻击性或营销功效。
- 用户要求校对时，不擅自重写个人风格和核心观点。

## 执行流程

1. 识别原文事实、主张、语气和不可变信息。
2. 将用户要求转成可检查的变化：长度、正式度、节奏、清晰度或结构。
3. 完成改写，消除歧义、重复和不必要的套话。
4. 对无法从原文确认但必须出现的信息使用占位符或提出一个问题。
5. 对照原文检查事实、立场、行动项和承诺是否漂移。

## 输出契约

默认只给改写后的完整文本。用户要求对照或修改说明时，再列出少量关键变化；不要逐句解释所有编辑动作。

## References 与 Templates

本 Skill 不依赖额外文件，以用户原文作为事实来源。

## 边界

本 Skill 不负责事实核验或平台策略。需要从零创作社媒内容时使用 `social-content`；需要先搭建长文逻辑时使用 `long-form-structure`。

## 示例

把催进度消息改得更温和时，可以调整称谓和请求方式，但不能删除截止时间或把“请确认”变成无行动要求的寒暄。

## 常见错误

- 润色后改变原意；
- 擅自补充看似合理的业务事实；
- 所有文本都改成同一种模板语气；
- 只做同义词替换，没有解决结构问题。
