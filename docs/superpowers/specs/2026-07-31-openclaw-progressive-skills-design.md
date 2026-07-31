# OpenClaw 风格渐进式 Skills 与分页读取设计

日期：2026-07-31

## 设计状态

本文取代 `2026-07-30-prompt-composer-skills-design.md` 中“将所有 Skill Body 拼接进 System Prompt”的设计。
核心身份、`AGENTS.md` 注入、Terminal Reporter 和既有工具调度仍沿用原设计；技能发现、Prompt 呈现和
`read_file` 行为以本文为准。

本设计参考 OpenClaw `main` 分支提交 `871abad910e76e76cf9f5922ce091ede076bbd3b`（2026-07-31）：

- Skills 说明：<https://github.com/openclaw/openclaw/blob/main/docs/tools/skills.md>
- Skill Prompt 契约：<https://github.com/openclaw/openclaw/blob/main/src/skills/loading/skill-contract.ts>
- Read 工具：<https://github.com/openclaw/openclaw/blob/main/src/agents/sessions/tools/read.ts>

## 目标

将 go-reagent 的技能系统从“全量正文注入”改为 OpenClaw 风格的渐进式读取：

```text
发现 SKILL.md
  -> 解析并校验结构化元数据
  -> 资格过滤、去重、排序和 Prompt 预算控制
  -> System Prompt 只注入 name/description/location/version 目录
  -> 模型根据 description 选择技能
  -> 模型调用 read_file(location) 读取 SKILL.md
  -> 文件较大时依据 continuation marker 分页读取
  -> 完整正文通过真实工具 Observation 进入上下文
  -> 模型按技能指令执行
```

必须满足：

- `SkillSummary`、`SkillSnapshot` 和 System Prompt 均不保存或包含 Body；
- 未被模型选择的技能正文不进入上下文；
- 模型只能通过真实 `read_file` 结果获得正文；
- `read_file` 具备 1-based 行分页能力；
- 技能目录和文件读取均受工作区边界保护；
- 当前 `.claw/skills` 继续兼容。

## 与 OpenClaw 的一致性边界

本期对齐以下核心行为：

- 内部保留结构化 Skill 集合；
- 必填 `name` 和 `description`；
- 使用 `metadata.openclaw` 表达资格要求；
- 会话/Run 开始时建立可用 Skill Snapshot；
- 使用 XML `<available_skills>` 目录；
- 目录提供 `name`、`description`、`location`、`version`；
- System Prompt 要求模型匹配后使用通用读取工具加载 `SKILL.md`；
- 目录受字符预算和数量预算限制；
- Read 工具支持 `offset`、`limit`，基础页最多 2000 行或 50 KiB。

本期不追求 OpenClaw 全功能同构，不实现全局、bundled、plugin、远程节点、Agent allowlist、Watcher、
`/skill:name` 或模型上下文感知的 32–128 KiB 自适应多页聚合。

## 端到端数据流

```text
workspace/skills/**/SKILL.md
workspace/.agents/skills/**/SKILL.md
workspace/.claw/skills/**/SKILL.md
                |
                v
        SkillLoader.Discover
                |
                | parse / validate / hash / filter / merge
                v
          SkillSnapshot
       +------------------+
       | []SkillSummary   |----> PromptComposer ----> System Prompt XML
       | Diagnostics      |
       +------------------+
                |                     |
                |                     v
                |             SkillPromptReport
                |               (预算结果)
                                           |
                                           v
                                     Thinking 选技能
                                           |
                                           v
                                     Action: read_file
                                           |
                               +-----------+-----------+
                               |                       |
                            文件结束               仍有后续
                               |                       |
                               v                       v
                            执行正文       read_file(offset=next)
```

## 技能目录来源与优先级

工作区内支持三个来源，优先级从高到低：

1. `skills/**/SKILL.md`
2. `.agents/skills/**/SKILL.md`
3. `.claw/skills/**/SKILL.md`

`.claw/skills` 是 go-reagent 现有兼容目录。更高优先级来源中的同名 Skill 覆盖低优先级来源，并产生
`skill_shadowed` 信息诊断。同一来源内出现重复名称时，该名称的所有冲突项均不进入 Snapshot，并产生
`skill_duplicate_name` 警告，避免依赖文件遍历偶然顺序。

各来源和来源内部均按规范化相对路径稳定排序，保证相同工作区生成相同 Snapshot 和 Prompt。

## 数据模型

### SkillSummary

```go
type SkillSummary struct {
    Name        string
    Description string
    Location    string
    Version     string
    Source      SkillSource
}
```

- `Name`：标准化技能名称；
- `Description`：模型选择技能的主要依据；
- `Location`：工作区相对的 `SKILL.md` 路径，使用 `/` 分隔；
- `Version`：文件完整内容的 SHA-256 短版本；
- `Source`：来源类型及优先级，不需要进入 Prompt。

`SkillSummary` 不包含 Body。

### SkillSnapshot

```go
type SkillSnapshot struct {
    skills      []SkillSummary
    diagnostics []SkillDiagnostic
}
```

Snapshot 建立后不可变，通过 `Skills()` 和 `Diagnostics()` 返回切片副本。Prompt 是否因预算被截断不属于发现
结果，由后述 `SkillPromptReport` 单独表达，Composer 不修改 Snapshot。

### SkillDiagnostic

```go
type SkillDiagnostic struct {
    Path     string
    Severity DiagnosticSeverity
    Code     string
    Message  string
}
```

诊断只包含工作区相对路径和脱敏信息，不包含 Body、环境变量值或工作区外路径内容。

## SKILL.md 解析和校验

最低合法格式：

```markdown
---
name: git-workflow
description: 处理 Git 提交、保存变更和版本控制操作
---

# 提交流程

执行指南正文。
```

校验规则：

- 文件第一行必须是独占一行的 `---`；
- 必须存在独占一行的结束 `---`；
- LF 和 CRLF 均可；
- Frontmatter 使用 `gopkg.in/yaml.v3`；
- `name` 必填，长度 1–64；
- `name` 只允许小写字母、数字以及作为单词分隔符的单个连字符；
- `description` 必填，去除首尾空白后长度 1–1024；
- Body 去除首尾空白后必须非空；
- 文件必须是合法 UTF-8 文本且不得包含 NUL；
- 文件最大 256 KiB；
- 只接受入口检查时为普通文件的 `SKILL.md`；
- 无 Frontmatter、非法 YAML 或缺失字段均记录诊断并跳过；
- 不再生成 `Unknown Skill` 或默认 description。

发现阶段允许临时读取整个受限文件，用于解析 Frontmatter、验证 Body 和计算版本，但读取完成后不得把 Body
保存在 `SkillSummary`、`SkillSnapshot` 或 Prompt 中。

版本使用整个原始文件的 SHA-256，Prompt 中呈现前 16 个十六进制字符：

```text
sha256:12ab34cd56ef7890
```

## OpenClaw Metadata 资格过滤

支持以下兼容格式：

```yaml
---
name: image-lab
description: 图片生成和编辑流程
metadata:
  openclaw:
    os:
      - darwin
      - linux
    requires:
      bins:
        - ffmpeg
      env:
        - OPENAI_API_KEY
---
```

本期支持：

- `metadata.openclaw.os`；
- `metadata.openclaw.requires.bins`；
- `metadata.openclaw.requires.env`；
- `disable-model-invocation`。

`disable-model-invocation` 是 Frontmatter 顶层布尔字段；前三类资格条件位于 `metadata.openclaw` 下。未知
Frontmatter 字段不影响标准字段解析。

```go
type SkillEnvironment struct {
    GOOS      string
    EnvLookup func(name string) bool
    BinLookup func(name string) bool
}
```

- Go 的 `windows` 映射为 OpenClaw 的 `win32`；
- 生产环境的 `BinLookup` 通过 `exec.LookPath` 检查，测试可注入确定性实现；
- 环境变量只检查是否存在，不读取或注入值；
- 不满足资格的 Skill 不进入模型目录，但产生 info 级诊断；
- `disable-model-invocation` 的 Skill 不进入目录。

元数据不能自动识别 Body 中未声明的工具引用。Skill 作者必须确保正文只要求当前 Agent 实际拥有的工具。
例如现有 `git-workflow` 正文要求 `bash`，而当前 Registry 未注册 `bash`，需要修改 Skill 或另行实现执行工具；
本期不会通过扫描自然语言正文猜测依赖。

## SkillLoader API

删除：

```go
func (s *SkillLoader) LoadAll() string
```

替换为：

```go
func (s *SkillLoader) Discover(env SkillEnvironment) (*SkillSnapshot, error)
```

Discover 执行：

1. 使用 `os.Root` 锚定工作区；
2. 按来源优先级扫描；
3. 读取受限的普通 `SKILL.md`；
4. 验证 UTF-8、NUL、大小、Frontmatter、字段和 Body；
5. 解析资格条件并过滤；
6. 计算 SHA-256；
7. 处理同名覆盖与冲突；
8. 按名称、Location 稳定排序；
9. 返回 Snapshot 和诊断。

工作区不存在任何技能时返回非 nil 空 Snapshot，不视为错误。只有无法建立工作区 Root 等系统级失败才返回 error；
单个 Skill 错误只进入 Diagnostics。

## Prompt Composer

当前：

```go
func (c *PromptComposer) Build() schema.Message
```

改为：

```go
func (c *PromptComposer) Build(snapshot *SkillSnapshot) (schema.Message, SkillPromptReport)
```

Composer 不再持有或调用 `SkillLoader`。它只负责：

1. 核心身份；
2. 根目录普通文件 `AGENTS.md`；
3. 已完成发现和过滤的 Skill Snapshot。

当 Snapshot 没有可呈现技能时，不输出技能标题或空 XML。

```go
type SkillPromptReport struct {
    IncludedSkills        int
    OmittedSkills         int
    ShortenedDescriptions int
    Truncated             bool
}
```

报告只描述本次 Prompt 渲染结果。Engine 用它记录 `skill_prompt_truncated` 诊断；报告不写回 Snapshot。

System Prompt 技能指导语：

```text
以下技能为特定任务提供专业执行指南。
当任务与某项 <description> 匹配时，必须先使用 read_file 读取该技能的 <location>。
如果 read_file 返回 "Use offset=N to continue"，必须继续读取，直到完整取得 SKILL.md 后再执行。
不要猜测未读取的技能内容，不要读取明显无关的技能。
如果 <version> 与之前看到的版本不同，必须重新读取该技能。
相对路径引用以 SKILL.md 所在目录为基准解析。
```

XML 格式：

```xml
<available_skills>
  <skill>
    <name>git-workflow</name>
    <description>处理 Git 提交、保存变更和版本控制操作</description>
    <location>.claw/skills/git-workflow/SKILL.md</location>
    <version>sha256:12ab34cd56ef7890</version>
  </skill>
</available_skills>
```

`name`、`description`、`location`、`version` 必须转义 `& < > " '`，Location 不允许由 Frontmatter
覆盖，只能来自真实扫描结果。

## Prompt 预算

项目默认值：

```go
const (
    maxSkillsInPrompt        = 150
    maxSkillsPromptChars     = 18_000
    maxSkillDescriptionChars = 1_024
)
```

预算算法：

1. 先构建所有 `name/location/version` 身份项；
2. 若身份项在预算内，用剩余预算加入 description；
3. description 按 UTF-8 rune 边界缩短；
4. 若所有身份项仍超预算，按稳定顺序截断技能数量；
5. 目录后附加被省略数量和原因；
6. 在 `SkillPromptReport` 中记录省略、缩短和截断情况；
7. 最终技能区块不得超过 `maxSkillsPromptChars`。

身份和 Location 优先于 description，避免少量超长描述挤掉大量技能。预算按 Unicode code point（Go rune）
计算，XML 标签、转义后的内容和省略说明全部计入；任何缩短都只能发生在 rune 边界。

## read_file 分页契约

### 输入

```go
type readFileArgs struct {
    Path   string `json:"path"`
    Offset *int   `json:"offset,omitempty"`
    Limit  *int   `json:"limit,omitempty"`
}
```

- `path`：必填，工作区相对路径；
- `offset`：可选，1-based 起始行，默认 1；
- `limit`：可选，最多返回行数，默认 2000，范围 1–2000。

基础页限制与 OpenClaw Read 工具一致：

```go
const (
    defaultReadFileMaxLines = 2000
    defaultReadFileMaxBytes = 50 * 1024
)
```

行数和字节数上限同时生效；显式 `limit` 不能突破 50 KiB 字节上限。50 KiB 统计最终工具输出，包含
continuation marker；确定存在后续内容时必须先为 marker 预留空间。

### 流式读取

不再使用 `io.ReadAll(io.LimitReader(..., 8000))`。改用 `bufio.Reader`：

1. `os.Root.Open(path)`；
2. `file.Stat` 确认普通文件；
3. 跳过 `offset-1` 行；
4. 逐行读取并验证 Context、NUL、UTF-8；
5. 仅加入不会突破行数和 50 KiB 上限的完整行；
6. 探测是否存在后续内容；
7. 有后续时附加 continuation marker。

不使用默认 `bufio.Scanner`，避免其默认 64 KiB 单行限制。

### continuation marker

```text
[Showing lines 1-420. Use offset=421 to continue.]
```

下一页：

```json
{
  "path": ".claw/skills/git-workflow/SKILL.md",
  "offset": 421
}
```

正文保持原始顺序，连续分页不得丢行或重复行。最后一页不包含 continuation marker。

### 边界行为

- `offset < 1`：参数错误；
- `limit < 1` 或 `limit > 2000`：参数错误；
- offset 超过 EOF：返回空字符串；
- 空文件：返回空字符串；
- 绝对路径或 Volume 路径：拒绝；
- 工作区逃逸：由 `os.Root` 拒绝；
- 目录、设备、FIFO：拒绝；
- 当前页包含 NUL：拒绝；
- 当前页不是合法 UTF-8：拒绝；
- 请求页的第一行连同必要的 continuation marker 无法放入 50 KiB：返回明确错误，避免产生无法继续的行
  offset；
- 仅在完整行和完整 UTF-8 边界停止；
- Context 取消时立即退出。

Tool Definition 明确说明单页 2000 行/50 KiB，以及 continuation marker 的继续方式。

## 技能渐进式读取

用户请求：

```text
帮我提交当前代码。
```

首轮 Thinking 只看到目录，判断 `git-workflow` 匹配，但不能虚构正文。

首轮 Action：

```json
{
  "name": "read_file",
  "arguments": {
    "path": ".claw/skills/git-workflow/SKILL.md"
  }
}
```

若工具返回：

```text
...前 420 行...

[Showing lines 1-420. Use offset=421 to continue.]
```

模型必须继续：

```json
{
  "name": "read_file",
  "arguments": {
    "path": ".claw/skills/git-workflow/SKILL.md",
    "offset": 421
  }
}
```

直到工具结果不再包含 continuation marker，模型才可以视为已经完整加载该技能。Body 只通过工具 Observation
进入 `contextHistory`，不会由 Composer 直接加入。

## Thinking 与 Action 衔接

当前 `thinkResp` 只打印，没有进入 `contextHistory`，Action 无法看到 Thinking 阶段选定的技能。每轮 Thinking
验证通过后，追加：

```go
contextHistory = append(
    contextHistory,
    *thinkResp,
    schema.Message{
        Role: schema.RoleUser,
        Content: "请依据上述计划进入 Action。匹配技能时先完整读取对应 SKILL.md。",
    },
)
```

Action 随后使用更新后的历史和真实工具定义。Thinking 仍不提供工具；它只能规划，不能声称 Skill 已读取。

## Snapshot 生命周期

- 每次 `AgentEngine.Run` 开始时获取可用工具信息并建立一次 Snapshot；
- 同一 Run 内复用 Snapshot；
- 下一次 Run 重新扫描；
- 本期不监听文件变化；
- `read_file` 读取调用时的当前文件内容；
- Version 用于提示模型识别跨 Run/刷新后的变化，不在通用 `read_file` 中执行 Skill 专属版本拦截。

这与采用通用 Read 工具的 OpenClaw 风格一致。Watcher 和中途 Snapshot 刷新留待后续。

## Engine 和依赖调整

`AgentEngine` 持有：

```go
type AgentEngine struct {
    provider    provider.LLMProvider
    registry    tools.Registry
    composer    *context.PromptComposer
    skillLoader *context.SkillLoader
    // existing fields...
}
```

Run 启动顺序：

1. 获取 Registry 工具定义；
2. 构造 `SkillEnvironment`；
3. `skillLoader.Discover`；
4. 记录 Diagnostics；
5. Snapshot 非空时确认 Registry 暴露 `read_file`，否则以明确配置错误终止 Run；
6. `composer.Build(snapshot)` 并记录 Prompt 渲染报告；
7. 构建 System/User 历史；
8. Thinking；
9. 将 Thinking 计划写回历史；
10. Action；
11. 根据工具结果继续循环。

Composer 不再持有 SkillLoader。Registry 无需注册新工具，只修改现有 `read_file`。

## 安全边界

- 发现和读取均使用 `os.Root`；
- Location 必须是扫描产生的工作区相对路径；
- Frontmatter 不能指定或覆盖 Location；
- 入口检查时的 symlink Skill 不进入 Catalog；
- 并发替换最多读取工作区内目标，不能逃逸工作区；
- XML 全量转义；
- Skill 文件大小最大 256 KiB；
- read_file 单次结果最大 50 KiB；
- 日志不输出 Body、API Key、环境变量值或外部文件内容；
- Skill Body 属于用户工作区授权的指令内容，但只能在模型选择并真实读取后生效。

## 诊断码

```text
skill_frontmatter_missing
skill_frontmatter_invalid
skill_name_missing
skill_name_invalid
skill_description_missing
skill_description_too_long
skill_body_empty
skill_file_too_large
skill_not_utf8
skill_binary_content
skill_duplicate_name
skill_shadowed
skill_os_ineligible
skill_missing_binary
skill_missing_environment
skill_model_invocation_disabled
skill_prompt_truncated
```

无效或不合格 Skill 不阻止 Agent Run。系统级 Discover 错误阻止 Run，并携带脱敏上下文。

## 文件调整

```text
internal/context/skill.go               # Skill 类型、Frontmatter 和基础校验
internal/context/skill_discovery.go     # 多来源扫描、优先级和资格过滤
internal/context/skill_snapshot.go      # Snapshot、版本和诊断
internal/context/skill_prompt.go        # XML 目录与预算
internal/context/composer.go            # 核心、AGENTS.md 和 Snapshot 组装
internal/context/*_test.go              # 解析、发现、过滤、预算和安全测试
internal/tools/read_file.go             # offset/limit 和流式分页
internal/tools/read_file_test.go        # 分页、边界、UTF-8 和安全测试
internal/engine/loop.go                  # Discover、Snapshot 和 Thinking 传递
internal/engine/loop_test.go             # 渐进读取上下文集成测试
```

## 测试策略

### Skill 发现

- 三种来源和优先级；
- 同源重复与跨源覆盖；
- 路径和名称稳定排序；
- LF、CRLF、多行 description；
- 无 Frontmatter、非法 YAML、缺失字段；
- name/description 边界；
- 空 Body、NUL、无效 UTF-8、超大文件；
- 静态内外 symlink 和工作区边界；
- OS、bin、env、disable-model-invocation 过滤；
- 相同内容版本稳定、内容变化版本变化。

### Prompt

- Snapshot 和 Prompt 均无 Body；
- XML 转义；
- Location 为工作区相对路径；
- Version 存在；
- 稳定排序；
- 空 Snapshot 不输出目录；
- 数量和字符预算；
- description 缩短和省略提示。

### read_file

- 默认第一页；
- offset 和 limit；
- 2000 行和 50 KiB 双重限制；
- continuation marker；
- 连续分页无丢行、无重复；
- offset 超过 EOF；
- 空文件和末尾换行；
- 中文 UTF-8 边界；
- 超长单行；
- NUL、非法 UTF-8；
- 绝对路径、路径逃逸、目录、symlink；
- Context 取消。

### Engine

- Run 启动时 Discover；
- System Prompt 只含目录，不含 Body；
- Thinking 计划进入 Action 历史；
- Action 可按 Location 调用 read_file；
- continuation 结果进入下一轮历史；
- Body 只通过 Observation 出现；
- 无技能时现有主循环不回归；
- 全仓 race 测试。

## 验收标准

- 不再把全部 Skill Body 拼接到 System Prompt；
- 程序内部保留无 Body 的结构化 Skill Snapshot；
- Prompt 使用 XML 技能目录并含 name/description/location/version；
- 模型通过 `read_file` 按需获得正文；
- `read_file` 支持 1-based offset、limit、2000 行/50 KiB 双上限；
- 大 Skill 可通过 continuation marker 完整分页读取；
- Thinking 选择能传递给 Action；
- 无效、不合格或冲突 Skill 不进入 Prompt且有诊断；
- 所有读取不能逃逸工作区；
- `gofmt -l cmd internal` 无输出；
- `go vet ./...` 通过；
- `go test -race -count=1 ./...` 通过。

## 不在本期范围

- 用户全局、bundled 和 plugin Skills；
- Agent allowlist；
- 文件 Watcher 和 Run 中途 Snapshot 刷新；
- `/skill:name` 显式调用；
- 远程节点；
- 图片读取；
- OpenClaw 32–128 KiB、最多四页的模型上下文感知自适应聚合；
- embeddings 或额外 LLM 技能路由阶段；
- 新增 bash/exec 工具。
