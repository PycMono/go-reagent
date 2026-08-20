---
name: writing-assistance
description: >-
  Produce a short, practical everyday text from clear user requirements. Use
  when the user needs a basic email, notice, message, summary, or other text
  and no more specific writing workflow is available. Triggers include "帮我写",
  "润色一下", "改得简短", "draft", "rewrite", and "polish". Do not use when
  the Writing Profile exposes social-content, rewrite-and-polish, or
  long-form-structure for the same task, or when the user only asks for advice.
---

# 写作协助

## 目标

根据用户给出的目的、受众和事实，快速生成一份可直接使用的日常文本。

## 必要输入

- 文本用途和受众；
- 必须包含的事实或动作；
- 用户指定的语言、语气、长度和格式。

信息足够时直接成稿。缺少会改变核心内容的条件时，只问一个最关键问题。

## 硬门禁

- 不虚构姓名、日期、价格、业绩、审批结果或用户经历。
- 缺失但必须出现的信息使用清楚的占位符，不暗中补全。
- 改写现有文本时，不改变用户未要求改变的事实和承诺强度。

## 执行流程

1. 识别文本目的、受众、希望对方采取的动作和限制。
2. 提取必须保留的事实，区分可调整表达与不可改动内容。
3. 选择符合场景的结构和语气，先写最重要的信息。
4. 检查长度、称谓、时间、数字、行动项和承诺是否与输入一致。
5. 信息充分时只给成稿；必要说明放在成稿之后且保持简短。

## 输出契约

默认输出一个完成版本。用户要求多个版本时，各版本应在语气、角度或长度上有清楚差异，不做同义词替换式变体。

## References 与 Templates

本 Skill 不依赖额外文件。Writing Profile 存在更具体的任务 Skill 时，应优先读取该 Skill。

## 边界

本 Skill 适合通用短文本，不承担平台内容策划、复杂改写策略或长文论证结构。需要专业事实时由用户或真实资料提供。

## 示例

用户给出会议改期事实并要求写通知时，应直接生成含原时间、新时间、原因和确认动作的通知，不先解释写作思路。

## 常见错误

- 在信息充分时连续追问；
- 为了“更完整”编造事实；
- 改写时悄悄加强承诺或改变立场；
- 已有更具体的 Writing Profile Skill 时仍使用本 Skill。
