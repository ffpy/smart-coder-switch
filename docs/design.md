# Smart Coder Switch 架构与关键设计

## 1. 系统定位

Smart Coder Switch 是一个兼容 OpenAI Chat Completions 与 Responses API 的模型路由代理。

客户端只使用逻辑模型，例如：

```json
{
  "model": "coder1"
}
```

代理根据配置自动选择四类档位：

| 档位 | 行为 |
|------|------|
| DIRECT | 最新用户消息强制路由，默认注入阶段感知首轮说明提示（优先级最高）。当消息包含图片时，注入合并提示（图片理解 + 首轮说明，单条消息带包裹标注） |
| LOW（默认） | 使用最经济的模型，不注入提示 |
| MEDIUM | 按 `medium-probability` 概率切换，只改模型不注入提示 |
| HIGH | 按 `high-probability` 概率切换，改模型并注入阶段感知复盘提示 |

系统完全不保存服务端会话，不分析历史消息轨迹。

## 2. 总体架构

```text
┌─────────────────┐
│ Coding Client   │
│ OpenCode 等     │
└────────┬────────┘
         │
         │ POST /v1/chat/completions 或 /v1/responses
         ▼
┌───────────────────────────────────┐
│ Smart Coder Switch                │
│                                   │
│  请求解析                         │
│      ↓                            │
│  DIRECT 检查（优先级最高）        │
│      ↓                            │
│  概率抽样（未命中 DIRECT 时）     │
│      ↓                            │
│  选择 DIRECT / LOW / MEDIUM / HIGH│
│      ↓                            │
│  改写实际模型                     │
│      ↓                            │
│  HIGH/DIRECT 时注入对应提示       │
│      ↓                            │
│  保存 Trace 并转发请求            │
└───────────────┬───────────────────┘
                │
                ▼
        ┌───────────────┐
        │  上游 Provider │
        └───────────────┘
```

一次请求的主要流程：

```text
接收请求
    ↓
读取逻辑模型
    ↓
DIRECT 检查 → 配置了 direct-model 且最后一条为真实用户输入 ?
    ↓ 是                                      ↓ 否
DIRECT 档位                           生成随机值 [0, 1)
    ↓                                        ↓
注入首轮说明提示                        区间判定 → LOW / MEDIUM / HIGH
    ↓                                        ↓
改写请求                                选择对应实际模型
    ↓                                        ↓
转发至上游                              HIGH 时注入复盘提示
                                             ↓
                                         改写请求
                                             ↓
                                         转发至上游
```

## 3. 路由设计

### 3.1 无状态概率与固定轮次模型

每次请求独立进行概率判定，不读取历史消息，不计算卡住评分，不分析失败或修复尝试。

DIRECT 优先于固定轮次和概率判定：

```text
最后一条消息是真实用户输入且配置了 direct-model ?
    │
    ├── 是 → DIRECT（强制路由，不参与概率判定）
    │
    └── 否 → 固定轮次判定
                │
         ┌──────┴──────┐
         ▼              ▼
   assistant 数能被   否 → 概率判定
   high-rounds 整除?         │
         │              ┌────┴────┐
        是             ▼          ▼
         ▼        随机值 <    随机值 <
      HIGH 档     high-prob  high-prob
                          + med-prob?
                            │      │
                           是     否
                            ▼      ▼
                       MEDIUM 档  LOW 档
```

固定轮次优先级：HIGH 固定轮次 > MEDIUM 固定轮次 > 概率判定。
固定轮次命中时无需随机抽样；未命中时回退到概率判定。

### 3.2 档位配置

```yaml
models:
  coder1:
    low-model: cheap-model
    medium-model: cheap-model          # 允许与 low-model 相同
    medium-probability: 0.10     # 每个模型独立配置概率
    high-model: powerful-model
    high-probability: 0.01
    high-rounds: 10              # 可选，assistant 消息数能被整除时强制 HIGH
    medium-rounds: 5             # 可选，assistant 消息数能被整除时强制 MEDIUM
    direct-model: stronger-model  # 可选，DIRECT 强制路由
    # low 概率：1 - 0.10 - 0.01 = 0.89
```

### 3.3 概率约束

- `medium-probability` 和 `high-probability` 必须显式配置（指针类型，无代码默认值）
- 每个逻辑模型独立配置概率，不同模型可以使用不同值
- 取值范围：[0, 1]
- 两个概率之和 ≤ 1
- 启动和热重载时校验，校验失败拒绝加载

### 3.4 DIRECT 强制路由

当逻辑模型配置了 `direct-model` 时，如果当前请求的最后一条消息为真实用户输入，代理会跳过概率判定，直接使用该模型。

DIRECT 检查过滤条件：

- 最后一条消息的 role 必须为 `user`
- 消息内容去除空白后不以 `ignored-user-input-prefixes` 列表中任一前缀开头
- 默认前缀列表：`<system-reminder>`、`[Compressed conversation section]`（可通过配置文件自定义）
- 非纯字符串的多模态内容被视为真实输入

DIRECT 是新任务入口的优化手段：用强模型做方向判断，后续请求回退到概率路由继续执行。

**DIRECT 首轮说明提示**

默认情况下（`direct-prompt-enabled` 未配置或为 `true`），DIRECT 会在请求末尾追加一条临时 `user` 消息。提示要求强模型先根据当前上下文和适用指令判断任务处于 PLAN 还是 BUILD：PLAN 或阶段不明时，可调用只读工具排查代码、Git、日志或配置，但不得修改代码或运行实施操作；BUILD 时先说明对问题的理解、判断和拟采取的动作，然后检查 `available_skills` 是否有适用的 Skill（确实适用且能提供编码规范或工作流指导时才加载，已加载的 Skill 直接复用无需重复加载，否则按常规推进，不得强行加载），并可按需调用工具或执行操作。说明保留在客户端会话历史中，供后续由 LOW/MEDIUM 接手参考。

提示正文：

```text
请先根据当前上下文和适用指令，判断当前任务处于 PLAN 阶段还是 BUILD 阶段；如存在明确的阶段指令，必须遵从。

PLAN 或阶段不明时，可调用只读工具进行排查，也可进行讨论、澄清、分析或计划/设计 Markdown 维护；不得修改代码或运行实施操作。BUILD 时先说明理解、判断与拟采取动作，再检查当前上下文列出的 available_skills 中是否有与当前任务匹配的 skill；仅当某个 skill 确实适用且能提供相关编码规范或工作流指导时才加载，若已加载则直接复用无需重复加载，没有合适的 skill 则按常规推进，不得强行加载，并可按需调用工具或执行操作。

不要强制使用 【Plan】、【Review】 或其他固定标题。在尚未获得实际操作结果前，不要声称已经检查、修改、执行或验证完成。
```

提示要求：
- 先判断 PLAN/BUILD；PLAN 或阶段不明时可调用只读工具，BUILD 阶段可按需调用工具
- PLAN 或阶段不明时不得修改代码、运行测试、构建、部署或其他实施操作
- BUILD 阶段执行前检查 available_skills：确实适用且能提供规范/工作流指导时才加载，已加载的 Skill 直接复用无需重复加载，否则按常规推进，不得强行加载
- 咨询类问题优先直接回答
- 禁止虚假完成声明（未执行前不得声称已完成）
- 不使用固定的 `【Plan】`/`【Review】` 模板

显式设置 `direct-prompt-enabled: false` 时，DIRECT 只改模型、不注入提示。此时若历史中包含 `【Review】` 或 `【Plan】` 标记，仍会按既有规则追加防复述指令。

启用 DIRECT 首轮提示时，即使历史包含 `【Review】`/`【Plan】` 标记，也只注入 DIRECT 提示，不再追加防复述指令（避免冲突）。

**DIRECT 图片理解提示**

当 DIRECT 档命中且最新用户消息包含图片（`type=image_url`）且 `image-prompt-enabled` 启用（默认）时，图片理解段落与首轮说明提示**合并为单条注入**（一条 `role: "user"` 消息，带包裹标注）。该提示要求模型：

1. 图片文字与结构提取：表格必须完整转写，保留图片中全部可见的列、行、数值和单元格文本，并使用 Markdown 表格或等价的结构化格式；列标题或单元格被截断时如实标注。代码、报错、公式、弹窗内容、字段名以及被红框/高亮/标注的内容按原文保留。
2. 非表格页面信息按相关性取舍：页面标题、筛选条件、按钮、页签、导航和品牌信息仅在与用户问题或表格含义有关时保留，无关内容可以省略。
3. 视觉信息按相关性取舍：仅描述影响问题判断的红框、高亮、异常状态或位置关系，不泛化描述无关的页面布局和装饰。
4. 问题定位：结合用户问题明确指出图片中的相关区域、证据和可得结论，不使用“如图所示”等依赖图片的指代。

输出以结构化文字呈现，作为后续对话上下文供可能不支持多模态输入的模型继续接手处理。表格内容不得因“精简”而省略，表格以外的信息按相关性规则取舍。推荐按“图片关键信息、完整表格转写、相关公式/弹窗/标注、与问题的关联”组织。图片理解完成后，再按下方阶段规则处理用户问题。

图片理解段落在合并提示中声明为**强制第一步（最高优先级）**：模型在输出任何回答或调用任何工具之前必须先完成图片转写，未完成转写输出前禁止直接回答用户问题。合并提示使用与普通 DIRECT/HIGH/防复述相同的统一包裹头（`WrapInstruction`）；统一包裹头措辞已去除"任务目标仍以前文真实用户消息为准 / 不要回复本段内容"等削弱性句子，并新增"本节要求的具体步骤必须执行并输出，不得跳过或忽略"，保证图片转写步骤不会被模型当作可选的辅助说明而忽略。

图片类型检测：
- OpenAI 格式：`type=image_url`（覆盖 HTTP URL 和 base64 data URI）

配置项：
- `image-prompt-enabled`（可选，默认 `true`）：控制是否注入图片理解段落
- 未配置或配置为 `true` 时：有图片的 DIRECT 请求注入合并提示
- 显式设置 `false` 时：退回普通 DIRECT 首轮说明提示，不注入图片理解段落

图片理解段落与首轮说明提示合并在同一条消息中，不再额外追加消息。当 `direct-prompt-enabled: false` 时，该合并提示不注入。

**非多模态档位历史图片过滤**

DIRECT 处理含图片请求后，`image_url` part 会保留在会话历史中。当后续请求路由到不支持多模态输入的模型（如 LOW 档的 DeepSeek）时，上游可能因无法解析 `image_url` 变体而报错（如 `unknown variant image_url, expected text`）。

为避免该问题，代理在转发前会过滤历史消息中的 `image_url` content part。过滤条件**需同时满足**：

1. `image-prompt-enabled` 启用（默认 `true`）：开启时 DIRECT 已把图片转写为结构化文字，过滤历史 `image_url` 不会丢失信息
2. 选中模型不支持多模态（不等于 `direct-model`）：默认仅 DIRECT 模型视为支持多模态输入

`image-prompt-enabled: false` 时图片未经前序模型转写，必须保留 `image_url` part 原样转发，避免图片信息丢失，因此**不过滤**。

过滤行为：

- 图文混合消息：删除 `image_url` part，保留 `text` part
- 仅图片消息：content 数组过滤后为空时，替换为占位文本（说明图片已由前序支持多模态的模型转写处理），避免空 content 触发上游格式校验错误
- 过滤只影响转发请求体，不修改客户端会话状态

过滤发生时，路由日志输出 `image_parts_stripped=true`，Trace 决策记录 `imagePartsStripped` 字段。

### 3.5 提示注入

代理在特定条件下会向请求追加提示消息。Chat Completions 注入位置在 `messages` 末尾；Responses 注入位置在 `input` 末尾，并保持原有 input item 不变。

**DIRECT 首轮说明提示**

DIRECT 档命中且 `direct-prompt-enabled` 启用时（默认），在消息列表**末尾追加**一条 `role: "user"` 的消息，要求强模型先判断 PLAN/BUILD；PLAN 或阶段不明时仅允许只读排查工具，BUILD 阶段执行前检查 `available_skills` 是否有适用的 Skill（确实适用且能提供编码规范或工作流指导时才加载，已加载的 Skill 直接复用无需重复加载，否则按常规推进，不得强行加载）。详见 3.4 节。

**DIRECT 图片理解提示**

当 DIRECT 档命中且最新用户消息包含图片时，图片理解段落与首轮说明提示合并为**一条** `role: "user"` 消息追加到末尾，使用统一包裹头（`WrapInstruction`）；图片理解段落声明为强制第一步（最高优先级）：模型必须先完成 OCR 提取、视觉描述和问题定位并输出转写结果，未完成前不得直接回答用户问题，再按阶段规则处理。详见 3.4 节。

**高档复盘提示（HIGH tier）**

`high` 档命中时，在消息列表**末尾追加**一条 `role: "user"` 的阶段感知消息。模型先判断 PLAN/BUILD：PLAN 或阶段不明时可进行只读排查、方案审查、风险排查与计划工作，不实施；BUILD 时先按 `【Review】` 格式输出回顾，再检查是否需要加载当前任务适用的 Skill：仅当上下文中的 `available_skills` 存在确实适用且能提供相关编码规范或工作流指导的 Skill 时才加载，已加载的 Skill 直接复用无需重复加载，没有合适 Skill 则忽略提醒并按常规推进，不得强行加载。提示内容不包含失败次数、回合信息、操作序列或任何评分数据。

提示以 `"role": "user"` 注入（非 `"role": "system"`），位置在所有已有消息之后。

**历史标记防复述指令**

LOW、MEDIUM、DIRECT（当且仅当 `direct-prompt-enabled: false` 时）三档在**最新一条 assistant 消息**的文本中包含 `【Review】` 或 `【Plan】` 标记时，会追加一条临时 user 防复述指令，要求直接执行历史指导，避免机械复述模板和标题。只检查最新一条 AI 消息，而不是扫描全部历史：工作指导只可能出现在最近一次 assistant 回复中，更早的标记已被执行或已被后续消息覆盖，不应再触发注入。标记检查同时支持字符串 content 与多模态数组 content（提取其中的 text part）。

可通过 `anti-repetition-prompt-enabled: true` 显式开启防复述注入。开启后，满足标记条件的请求会追加防复述内容。未配置时默认为 `false`（禁用）。

**注入优先级**

为确保每次请求最多追加一条临时 user 消息，各类注入按优先级互斥：

1. DIRECT 首轮提示（启用时）优先级最高，抑制历史标记防复述指令
2. HIGH Review 优先于 DeepSeek 工具调用边界兼容补丁
3. 防复述指令优先于 DeepSeek 兼容补丁

DeepSeek 工具调用边界兼容补丁（`FixMissingReasoningContent`）在请求处于 tool-call 边界、目标模型为 DeepSeek 系、且历史 `assistant.tool_calls` 消息缺少 `reasoning_content` 时触发。为避免占位文本污染完整历史（历史中每条 `tool_calls` 消息都被补上相同占位文本，导致模型上下文堆积大量无意义内容），只修复**最近一个 user 消息之后**的连续工具调用段（与 `detectToolBoundary` 判定范围一致），更早的工具调用段不再补充；该补丁只修正请求体字段、不追加消息。

### 3.6 Responses API 协议支持

代理同时支持 Chat Completions 与 OpenAI Responses API（`POST /v1/responses`），两者共享同一套无状态路由判定。

Responses 请求的 `input` 字段可以是字符串，也可以是 input item 数组。代理会将 `input` 归一化为路由视图（role message、`input_text`、`input_image`、assistant `output_text`、`function_call`、`function_call_output` 等常见条目），复用 Chat 已有的 DIRECT / LOW / MEDIUM / HIGH 路由、续接识别和 ignored-prefix 过滤；转发时仍使用原始 Responses 格式，不做 Chat 转换。

Responses 下的行为差异：

- assistant 轮次按本次请求可见的 `input` 内 `role=assistant` / `output_text` 等条目标识统计
- DIRECT 判定只看最后一条 input item 是否为真实用户输入（`role=user`/`developer` 消息或 `text`、`input_text` 条目）；tool 输出、assistant 回复等非用户输入条目跳过 DIRECT
- HIGH / DIRECT / 防复述等提示以新的 input item 追加在末尾
- 图片检测识别 `input_image` item；路由到非多模态模型时剥离 `input_image` 条目并在剩余为空时注入占位 user 文本
- `initial_previous_response_id` / `previous_response_id` 仅在本次请求内保留，不触发跨请求历史查询或合并
- `instructions` 原样透传，不与代理提示合并或覆盖

不支持与不兼容：

- 不保存服务端会话状态，不根据 `previous_response_id` 恢复历史
- 不做 Chat / Responses 格式互转
- Chat 专用的 DeepSeek `reasoning_content` 兼容补丁不应用于 Responses

## 4. 配置快照与热重载

运行时配置使用不可变快照。

每个请求开始时读取一次配置，并在整个请求处理期间使用同一个版本。

热重载流程：

```text
读取 YAML
    ↓
解析和校验
    ↓
校验模型映射和概率参数
    ↓
原子替换当前配置快照
```

如果新配置无效：

- 不替换当前配置
- 不增加配置版本
- 当前服务继续使用旧配置

Admin 接口：

```text
GET  /admin/config/version    # 配置版本
GET  /admin/config            # 查看当前配置（YAML）
POST /admin/config/reload     # 热重载配置
GET  /admin/stats/models      # 模型调用统计
POST /admin/stats/models/reset # 重置统计
```

`listen.address` 不能热重载，修改后需要重启进程。

## 5. Trace 与日志

Trace 默认保存在：

```text
./log/traces
```

每条记录使用时间作为目录名：

```text
log/traces/
└── 20260716-151230.123456789-000001/
    ├── request.json
    └── decision.json
```

其中：

- `request.json` 保存客户端原始请求
- `decision.json` 保存路由结果和主要判断依据，包含逻辑模型、实际选择的模型、档位、路由原因、概率配置和随机值

`log/` 是运行时目录，不提交到 Git。

Trace 用于调试和路由分析。Trace 保存失败不应阻止正常代理请求。

### 5.1 日志与 Trace 关联

每条请求在路由决策日志中输出 `trace_dir` 字段，值即为 Trace 目录名：

```text
route logical model selected_tier=MEDIUM ... trace_dir=20260716-151230.123456789-000001
```

通过 `trace_dir` 可从日志行唯一定位到对应的 Trace 目录：

```bash
# 从日志找到 trace_dir
grep "selected_tier=MEDIUM" log/app.log | head -1

# 查看对应 Trace
ls log/traces/20260716-151230.123456789-000001/
# request.json    decision.json
```

请求转发完成后还会输出一条上游响应日志：

```text
upstream response selected_model=glm-5 status_code=200 trace_dir=20260716-151230.123456789-000001
```

同一请求的两条日志通过相同的 `trace_dir` 关联。

### 5.2 路由决策日志字段

| 字段                     | 说明                                       |
| ------------------------ | ------------------------------------------ |
| `logical_model`          | 客户端请求的逻辑模型名                     |
| `selected_tier`          | 选中档位：LOW / MEDIUM / HIGH / DIRECT     |
| `selected_model`         | 实际转发的模型名                           |
| `route_reason`           | 路由原因                                   |
| `random_value`           | 随机抽样值（档位判据）                     |
| `prompt_injected`        | 是否注入了提示（复盘或新任务计划）         |
| `prompt_injection_kind`  | 注入提示的类型：`high_tier_review`/`direct_first_response` |
| `image_prompt_injected`  | 是否注入了图片理解提示（DIRECT 档含图片时） |
| `image_parts_stripped`   | 是否过滤了历史消息中的 image_url part（非多模态模型且 image-prompt-enabled 启用时） |
| `body_size`              | 请求体字节数                               |
| `medium_probability`     | 中档概率配置                               |
| `high_probability`       | 高档概率配置                               |
| `trace_dir`              | Trace 记录目录名（用于关联文件）           |
> **route_reason 取值**：`probability_low`、`probability_medium`、`probability_high`、`rounds_high`、`rounds_medium`、`direct_route`、`continuation_probability`

## 6. 模型调用统计

`internal/stats` 包提供进程内的并发安全计数器。统计特性：

- **仅记录已配置逻辑模型的请求**：未知模型原样转发且不计数
- **双维度统计**：按实际模型和逻辑模型分别累计
- **进程内累计**：重启后归零，热重载不清零
- **成功判定**：上游返回 HTTP 2xx 视为成功

通过 Admin API `GET /admin/stats/models` 查询，`POST /admin/stats/models/reset` 重置。

## 7. 关键设计取舍

### 无状态路由

放弃了对历史消息、失败和修复轨迹的分析。每次请求独立抽样。

优点：
- 实现简单，代码量大幅减少
- 无状态，不需要会话数据库
- 不会因轨迹分析引入误判

代价：
- 高档请求是纯随机事件，不针对卡住场景精准触发
- 连续卡住的请求可能连续落在低档

### 四档路由（DIRECT + 三档概率）

四档互斥，判定顺序为：DIRECT → HIGH → MEDIUM → LOW。

- DIRECT 为新增强制路由，不参与概率判定，用于新任务入口的强模型方向判断
- LOW 是默认档位，用于大多数请求以控制成本
- MEDIUM 提供适中的能力提升，不注入提示
- HIGH 是最高能力档位，并自动注入阶段感知复盘提示，用于需要深入分析的场景

### 配置快照

已开始的请求继续使用旧配置，新请求使用新配置，避免热重载中断普通请求或 SSE 流。

### 固定提示

两种注入提示都是固定内容，不包含任何请求上下文分析。这避免了提示内容随轨迹变化引入不确定性，也简化了实现。

### CLI 子命令

通过子命令访问 Admin API：

```text
reload       热重载配置
config       查看当前配置（YAML）
stats        查看模型调用统计
stats reset  重置模型调用统计
version      查看配置版本
```

子命令从配置文件读取 `listen.address` 构造 Admin API URL，不依赖服务运行状态之外的额外配置。
