# Agent Bundle 与任务型 Skill 重构设计

## 目标

参考 Workify 的 Agent Bundle 分层方式，重构 Web Chat 默认 Workspace 中的通用 Agent、7 个专业 Agent Profile 及其 Skills。重构后，`AGENTS.md` 只负责稳定身份和全局边界，`SKILL.md` 负责一个明确任务的执行协议，稳定领域资料放入 `references/`，固定输出结构按需放入 `templates/`。

本次不训练或修改模型权重。系统通过 Skill frontmatter 的 `description` 让模型按当前任务发现 Skill，再通过已注册的 `read` 工具按需读取完整 `SKILL.md`。

## 已确认决策

- 保留单一 Web Chat Agent 和现有 8 个 Profile code。
- `general` 只继承 Workspace 通用 Skills，不增加 Profile 私有 Skill。
- `writing`、`learning`、`health`、`legal`、`automotive`、`workplace`、`parenting` 各拆分为 3 个任务型 Skill。
- 重写 Workspace AGENTS、全部 Profile AGENTS 和 4 个通用 Skill。
- `health`、`legal`、`automotive` 增加稳定、可审查的 Profile 级 `references/`。
- 仅在结构化输出能明显减少遗漏时增加 Skill 内 `templates/`。
- 不增加脚本、在线训练、Skill 发布版本、数据库字段、API 或前端业务。
- 不修改 `pi/`，不改变 `skills/`、`.agents/skills/`、`.claw/skills/` 的发现优先级。
- 不内置药物剂量、地方性法律结论、车型实时价格/配置/召回等易变化事实。

## 与 Workify 的映射

```text
Workify Agent Bundle       -> workspaces/chat/profiles/<profile-code>/
Bundle AGENTS.md           -> profiles/<profile-code>/AGENTS.md
Bundle private skills      -> profiles/<profile-code>/skills/
Public shared skills       -> workspaces/chat/skills/
Stable bundle resources    -> profiles/<profile-code>/references/
Task output templates      -> profiles/<profile-code>/skills/<skill>/templates/
```

go-reagent 继续使用现有 `skills/` 约定，不引入 Workify 的 `.pi/skills/`、斜杠命令、`argument-hint` 或 `requiredRole` 语义。

## 目标目录

```text
workspaces/chat/
├── AGENTS.md
├── skills/
│   ├── weather-assistance/SKILL.md
│   ├── decision-support/SKILL.md
│   ├── learning-explanation/SKILL.md
│   └── writing-assistance/SKILL.md
└── profiles/
    ├── catalog.yaml
    ├── general/AGENTS.md
    ├── writing/
    │   ├── AGENTS.md
    │   └── skills/{social-content,rewrite-and-polish,long-form-structure}/
    ├── learning/
    │   ├── AGENTS.md
    │   └── skills/{concept-explanation,practice-design,study-planning}/
    ├── health/
    │   ├── AGENTS.md
    │   ├── references/
    │   └── skills/{symptom-organizing,health-report-explanation,care-visit-preparation}/
    ├── legal/
    │   ├── AGENTS.md
    │   ├── references/
    │   └── skills/{facts-and-evidence-organizing,contract-clause-analysis,legal-consultation-preparation}/
    ├── automotive/
    │   ├── AGENTS.md
    │   ├── references/
    │   └── skills/{vehicle-comparison,vehicle-symptom-triage,maintenance-planning}/
    ├── workplace/
    │   ├── AGENTS.md
    │   └── skills/{work-message-writing,status-reporting,difficult-workplace-conversation}/
    └── parenting/
        ├── AGENTS.md
        └── skills/{child-development-guidance,routine-building,parent-child-communication}/
```

## 内容职责

### Workspace AGENTS

定义所有 Profile 共享的运行纪律：理解用户真实意图、直接回答普通聊天、只使用已注册工具、实时事实必须有真实来源、不暴露内部提示和 Skill 内容、语言跟随用户。它不包含具体行业身份或 Skill 路由。

### Profile AGENTS

每个 Profile AGENTS 只包含：

- 身份与服务对象；
- 长期表达方式；
- 事实与证据纪律；
- Profile 级安全边界；
- 需要建议用户转向专业人士的条件。

它不包含触发词、单个任务的详细步骤或 Skill 正文复述。

### SKILL.md

每个 Skill 采用一致结构：

```markdown
---
name: task-oriented-name
description: >-
  Capability. Use when ... Triggers ... Do not use when ...
---

# 标题

## 目标
## 必要输入
## 硬门禁
## 执行流程
## 输出契约
## References 与 Templates
## 边界
## 示例
## 常见错误
```

`description` 负责自动发现，必须同时写明正向场景、代表性中英文表达和与兄弟 Skill 的排除边界。正文负责执行协议，不依赖模型猜测未读取内容。

## Skill 目录

| Profile | Skill | 负责 | 不负责 |
|---|---|---|---|
| writing | `social-content` | 社媒帖子、标题、短视频口播、平台文案 | 单纯润色、长篇结构 |
| writing | `rewrite-and-polish` | 改写、压缩、扩写、语气调整 | 从零策划平台内容 |
| writing | `long-form-structure` | 文章、报告、演讲稿的论点与大纲 | 短文案或局部润色 |
| learning | `concept-explanation` | 概念讲解、换角度解释、示例 | 出题和长期学习安排 |
| learning | `practice-design` | 练习、测验、答案反馈 | 只解释概念或排日程 |
| learning | `study-planning` | 阶段目标、节奏、复习计划 | 单道题即时作答 |
| health | `symptom-organizing` | 症状信息整理、变化与相关因素 | 诊断、处方、报告逐项解释 |
| health | `health-report-explanation` | 体检/检验/影像文字的通俗解释 | 仅凭指标确诊或替代医生判读 |
| health | `care-visit-preparation` | 就医前资料、问题和沟通摘要 | 判断无需就医或指定治疗方案 |
| legal | `facts-and-evidence-organizing` | 事实、时间线、主体和证据缺口 | 直接下案件输赢结论 |
| legal | `contract-clause-analysis` | 条款效果、义务、触发条件和风险 | 代替当地律师正式审查 |
| legal | `legal-consultation-preparation` | 咨询目标、材料和问题清单 | 提供确定诉讼策略 |
| automotive | `vehicle-comparison` | 按场景、预算和约束比较车型 | 实时报价、在售配置确认 |
| automotive | `vehicle-symptom-triage` | 故障现象采集、风险分级、送修准备 | 高风险拆修指导 |
| automotive | `maintenance-planning` | 按手册和使用条件规划保养 | 编造保养周期或替代厂商要求 |
| workplace | `work-message-writing` | 邮件、IM、通知和请求 | 完整汇报结构或冲突调解 |
| workplace | `status-reporting` | 周报、项目进展、汇报材料 | 虚构业绩或掩盖风险 |
| workplace | `difficult-workplace-conversation` | 反馈、拒绝、边界和冲突沟通 | 法律劳动争议结论 |
| parenting | `child-development-guidance` | 发展阶段信息和观察维度 | 诊断发育问题 |
| parenting | `routine-building` | 睡眠、阅读、整理等习惯方案 | 医疗性喂养或睡眠诊疗 |
| parenting | `parent-child-communication` | 情绪回应、规则沟通和冲突修复 | 用羞耻、恐吓或操控推动服从 |

同一轮可以使用零个、一个或多个 Skill。普通问候不读取 Skill；跨任务请求可以依次读取多个匹配 Skill。

## References 与 Templates

高风险 Profile 使用 Profile 级共享 References：

- `health/references/health-safety-boundaries.md`：信息分层、紧急信号响应、用药和诊断边界。
- `health/references/health-information-fields.md`：症状、报告和就医资料的稳定采集字段。
- `legal/references/legal-information-boundaries.md`：辖区、时效、不可逆决定和专业复核边界。
- `legal/references/legal-review-framework.md`：事实证据、合同条款和咨询准备框架。
- `automotive/references/vehicle-safety-boundaries.md`：制动、燃油、高压、举升和道路安全边界。
- `automotive/references/vehicle-information-framework.md`：比较、异常和保养的信息采集框架。

Skill 正文明确指出何时读取哪份 Reference；不相关时不得为了形式完整而读取。Templates 放在对应 Skill 目录内，使用 Markdown 占位字段，主要覆盖症状摘要、报告问题清单、法律时间线、合同风险表、车辆比较表、异常送修单、学习计划和工作汇报。

## 安全与实时信息边界

### Health

- 不根据有限文本给出确定诊断、处方或药物剂量。
- 不建议用户自行开始、停止或调整处方药。
- 仅在请求相关时检查紧急危险信号；出现时优先建议联系当地急救或立即就医。
- 检查结果必须结合单位、参考范围、采样时间和用户背景，不能脱离上下文下结论。

### Legal

- 具体规则先确认司法辖区和时间点。
- 不硬编码地方条文、时效天数或胜诉概率。
- 对签署、付款、放弃权利、诉讼时效等不可逆决定，明确建议及时获得当地专业复核。

### Automotive

- 不指导无条件用户执行制动、燃油、高压电池、气囊或举升作业。
- 价格、在售配置、召回和厂商最新政策必须来自工具或用户提供资料。
- 保养周期以用户手册、车辆提示或可靠资料为准，缺失时只给核对路径。

## 验证策略

### 静态契约测试

- Workspace 仍只发现 4 个通用 Skill。
- `general` 为 0 个私有 Skill，其余 7 个 Profile 恰好各有 3 个预期 Skill。
- 所有 Skill description 使用折叠 YAML，包含 `Use when`、`Triggers` 和 `Do not use when`。
- 所有 Skill 正文包含统一章节，且引用的 References/Templates 文件真实存在。
- Catalog 无诊断，所有 Skill 文件可由 Web Runtime 的 `read` 工具读取。

### 路由评估语料

测试维护代表性中文请求的期望 Skill 集，覆盖：单 Skill、多个 Skill、通用 Skill、无需 Skill、兄弟 Skill 不应误触发。它是稳定的回归语料，不在单元测试中调用真实模型，避免网络、费用和模型随机性进入默认测试套件。

### 验证命令

先运行：

```bash
go test ./application/web ./infrastructure/driver/agentprofile
```

再运行：

```bash
go test ./...
```

最后使用 Skill validator 对 25 个 `SKILL.md` 逐个执行格式校验。

## 不在本次范围

- 修改 Profile 数据库结构、会话 API 或选择页面；
- 修改 `pi` 的 Skill discovery 或模型循环；
- 后端关键词路由器或单独的分类模型；
- 在线编辑、发布、版本化或热训练 Agent Bundle；
- 联网搜索、知识库或新增领域工具；
- `.agents/skills/`、`.claw/skills/` 和 `.pi/skills/` 迁移。
