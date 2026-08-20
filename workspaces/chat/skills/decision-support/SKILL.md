---
name: decision-support
description: >-
  Compare options using the user's goals, constraints, evidence, and tradeoffs.
  Use when the user needs a choice, ranking, shortlist, or recommendation.
  Triggers include "怎么选", "哪个好", "帮我比较", "值不值得", "compare",
  "choose", and "recommend". Do not use when the user only requests a factual
  lookup, has already chosen and only needs execution, or a more specific
  Profile Skill such as vehicle-comparison is available.
---

# 决策协助

## 目标

把模糊的“哪个好”转化为可核对的目标、约束和权衡，给出带条件的建议，同时保留最终选择权。

## 必要输入

- 待比较的选项或需要形成选项的范围；
- 用户最重要的目标、硬约束和偏好；
- 对结论有实质影响的预算、期限或风险承受度。

能从上下文推断时不重复询问。缺少关键条件时，先问影响最大的一个问题。

## 硬门禁

- 不把模型印象、广告描述或未经确认的信息写成事实。
- 不替用户决定医疗、法律、财务等不可逆高风险事项。
- 依赖实时价格、库存、政策或性能数据时，必须使用真实工具或用户资料；没有来源就标记未知。

## 执行流程

1. 用一句话确认决策目标，并区分硬约束与偏好。
2. 选择 3 至 6 个真正影响目标的比较维度，避免无关参数堆砌。
3. 将证据、用户偏好和推断分别标识；缺失数据保留为空，不自行补齐。
4. 先淘汰违反硬约束的选项，再比较剩余选项的主要收益与代价。
5. 给出一个有条件的首选和备选，说明什么条件变化会改变建议。

## 输出契约

简单选择直接输出“建议 + 两个关键理由 + 主要代价”。复杂选择使用紧凑比较表，随后给出首选、适用条件、备选和待核实信息。

## References 与 Templates

本 Skill 不依赖额外文件。专业 Profile 提供更具体的比较 Skill 时，应改用该 Skill 的 References 与 Templates。

## 边界

建议是基于现有信息的决策支持，不宣称存在唯一正确答案。用户只想了解事实时直接回答事实，不强行扩展成决策矩阵。

## 示例

用户问“通勤坐地铁还是开车”，应先抓住时间、总成本、稳定性和停车条件，再给条件化建议；不要先罗列所有交通方式。

## 常见错误

- 每个维度权重相同，忽略用户真正关心的目标；
- 用大量表格掩盖关键数据缺失；
- 只给优点，不说明首选的主要代价；
- 在专业 Profile 已有专用比较流程时仍重复使用本 Skill。
