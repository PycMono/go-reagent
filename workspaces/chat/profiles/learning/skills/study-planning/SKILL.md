---
name: study-planning
description: >-
  Build a realistic multi-session learning plan with milestones, practice,
  review, and observable completion criteria. Use when the user has a learning
  goal over days or weeks and needs priorities or scheduling. Triggers include
  "学习计划", "备考安排", "每天学什么", "六周复习", "study plan", and
  "learning schedule". Do not use when the task is a one-off explanation, a single homework
  problem, or only generating a set of exercises.
---

# 学习计划

## 目标

把学习目标转化为适合用户基础和时间条件的阶段计划，并通过可检查成果持续调整。

## 必要输入

- 目标、截止时间和成功标准；
- 当前基础、可用资料和每周可投入时间；
- 已知薄弱点、固定限制和偏好的学习方式。

## 硬门禁

- 不承诺通过考试、达到分数或在不现实时间内掌握完整领域。
- 不虚构最新考试大纲、报名政策或课程资源。
- 不把计划排满全部可用时间，必须保留复盘和缓冲。

## 执行流程

1. 定义终点能力和当前差距，优先排序高价值内容。
2. 划分少量阶段，每阶段包含学习、练习、回忆和检测。
3. 将阶段任务分配到用户真实可用时段，控制单次负担。
4. 为每阶段设置可观察完成标准，而非只写“学完”。
5. 安排复盘点，根据正确率、完成度或输出质量调整后续计划。

## 输出契约

输出目标假设、阶段表、每周节奏、检查标准和调整规则。日程必须能看出做什么、做到什么程度以及如何判断完成。

## References 与 Templates

需要结构化计划时，读取 `profiles/learning/skills/study-planning/templates/study-plan.md` 并按周期删减字段。

## 边界

计划不替代用户执行，也不保证结果。若用户只想理解概念或获取题目，分别使用 `concept-explanation` 或 `practice-design`。

## 示例

六周备考计划应先确认目标和每天一小时的限制，再按诊断、核心内容、专项练习、模拟和复盘分阶段，而不是每天平均分配所有科目。

## 常见错误

- 任务按章节平均分配，没有优先级；
- 只有阅读，没有回忆和练习；
- 完成标准写成“认真掌握”；
- 没有缓冲和调整机制。
