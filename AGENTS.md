# Smart Coder Switch - Agent 上下文

## 项目定位

Smart Coder Switch 是一个面向 AI 编程 Agent 的智能模型路由代理。

客户端使用逻辑模型（如 `coder1`），代理根据配置自动选择四类档位：

- **DIRECT**：最新用户消息强制路由，默认注入首轮说明提示（优先级最高）。当消息包含图片时，注入合并提示（图片理解 + 首轮说明，单条消息带包裹标注）
- **LOW（默认）**：处理常规编码任务，成本更低
- **MEDIUM**：按概率切换，只改模型不注入提示
- **HIGH**：按概率切换，自动注入复盘提示

代理不保存会话状态，每次请求独立判定。不分析历史消息轨迹。

## 技术栈

- Go 1.21+
- HTTP 标准库（无框架）
- YAML 配置
- SSE 流式响应

## 目录结构

```text
cmd/smart-coder-switch/
    main.go                  # 入口
    commands.go              # CLI 子命令
internal/
    admin/                   # Admin 接口（配置版本、热重载、统计查询）
    buildinfo/               # 构建版本信息注入
    config/                  # 配置加载、校验、快照
    protocol/openai/         # OpenAI 协议解析、模型改写、提示注入
    proxy/                   # HTTP 代理、上游转发、热重载管理
    routing/                 # 路由决策、提示构建
    stats/                   # 模型调用统计
    trace/                   # 请求和决策 Trace 保存
scripts/
    build.sh                 # 构建
    deploy.sh                # 编译 + 重启服务
    run.sh                   # 快速重启
docs/
    design.md                # 架构和关键设计
```

## 常用命令

```bash
# 测试
go test -count=1 ./...
go test -race -count=1 ./...

# 静态检查
go vet ./...

# 构建
./scripts/build.sh


# 运行
./dist/smart-coder-switch
./dist/smart-coder-switch -config /path/to/config.yaml
./dist/smart-coder-switch -version

# CLI 子命令
./dist/smart-coder-switch reload        # 热重载配置
./dist/smart-coder-switch config        # 查看当前配置
./dist/smart-coder-switch stats         # 查看模型调用统计
./dist/smart-coder-switch stats reset   # 重置统计
./dist/smart-coder-switch version       # 查看配置版本
```

## 配置要点

配置文件：`config.yaml`（基于 `config.example.yaml` 复制）

关键配置项：

```yaml
# 系统注入消息过滤前缀（可选）
# ignored-user-input-prefixes:
#   - <system-reminder>
#   - [Compressed conversation section]

listen:
  address: 0.0.0.0:18082

upstream:
  base-url: https://your-provider.example.com/

models:
  coder1:
    low-model: low-cost-model
    medium-model: medium-model-name
    medium-probability: 0.10
    high-model: powerful-model-name
    high-probability: 0.01
    high-rounds: 10              # 可选，assistant 消息数能被整除时强制 HIGH
    medium-rounds: 5             # 可选，assistant 消息数能被整除时强制 MEDIUM
    # direct-model: stronger-model  # 可选，新任务强制路由
    # direct-prompt-enabled: true   # 可选，控制 DIRECT 是否注入首轮说明提示，默认 true
    # image-prompt-enabled: true                 # 可选，控制图片理解提示注入，默认 true
```

热重载：

```bash
curl -X POST http://127.0.0.1:18082/admin/config/reload
```

注意：`listen.address` 修改后需要重启进程。

## HTTP 接口

```text
GET  /health                  # 健康检查
POST /v1/chat/completions     # Chat Completions 代理
POST /v1/responses            # Responses API 代理（无状态路由）
GET  /admin/config/version    # 配置版本
GET  /admin/config            # 查看当前配置（YAML）
POST /admin/config/reload     # 热重载配置
GET  /admin/stats/models      # 模型调用统计
POST /admin/stats/models/reset # 重置统计
```

`Authorization` 请求头原样转发给上游。

## 路由逻辑概要

采用无状态概率与固定轮次路由。每次请求独立抽样，不读取历史消息，不计算卡住评分。Chat Completions 与 Responses 共用同一套路由语义：Responses 的 `input` 会被归一化为 `[]openai.Message` 视图，复用 assistant 计数、DIRECT 判断、续接识别和 ignored-prefix 过滤。

- **DIRECT**：配置 `direct-model` 且最后一条消息为真实用户输入时，直接使用指定模型（优先级最高）。默认注入首轮说明提示，要求先说明理解与判断，再允许工具调用；显式设置 `direct-prompt-enabled: false` 时关闭提示注入。当最新用户消息包含图片（`type=image_url`）且 `image-prompt-enabled` 启用（默认）时，注入合并提示：图片理解段落（OCR 提取 + 视觉描述 + 结构化输出，供后续不支持多模态输入的模型接手处理）与首轮说明提示合并为单条 user 消息，并带统一包裹标注；`image-prompt-enabled: false` 时退回普通首轮说明提示
- **HIGH 固定轮次**：配置 `high-rounds` 且 assistant 消息数能被整除时，强制切换 HIGH 模型并注入复盘提示
- **MEDIUM 固定轮次**：配置 `medium-rounds` 且 assistant 消息数能被整除时（且未命中 HIGH 轮次），强制切换 MEDIUM 模型，不注入提示
- **LOW（默认）**：使用 `low-model` 模型，不注入提示
- **MEDIUM 概率**：按 `medium-probability` 概率命中时切换 `medium-model` 模型，不注入提示
- **HIGH 概率**：按 `high-probability` 概率命中时切换 `high-model` 模型，并自动注入复盘提示

```text
最新用户消息为真实输入且配置了 direct-model ?
    ↓ 是 → DIRECT
    ↓ 否 → assistant 数能被 high-rounds 整除?
              ↓ 是 → HIGH
              ↓ 否 → assistant 数能被 medium-rounds 整除?
                        ↓ 是 → MEDIUM
                        ↓ 否 → 随机值 [0, 1) → 区间判定 → LOW / MEDIUM / HIGH
```

`low-model` 和 `medium-model` 可以配置为同一个实际模型。

详细设计见 `docs/design.md`。

## 开发约定

### 代码风格

- Go 标准格式化：`gofmt -w`
- 无外部框架依赖
- 错误处理：返回 error，不 panic
- 日志：使用 `log/slog`

### Git 提交规范

- 提交消息必须使用中文
- 格式：`<类型>: <描述>`
- 类型示例：
  - `add: 新功能`
  - `mod: 调整现有功能`
  - `fix: 修复 bug`
  - `opt: 性能或代码改进`
  - `docs: 文档更新`

### 配置变更

- 新增配置项需要：
  1. 更新 `internal/config/config.go`
  2. 更新 `config.example.yaml`
  3. 添加校验逻辑（如需要）
  4. 更新 README.md 说明

### 热重载规则

- 有效配置：原子替换，版本号 +1
- 无效配置：保留旧配置，不影响服务
- `listen.address` 不能热重载

## Trace 文件

开启 Trace 后，每条请求保存：

```text
./log/traces/YYYYMMDD-HHMMSS.纳秒-序号/
├── request.json    # 原始请求
└── decision.json   # 路由决策详情
```

Trace 用于调试路由逻辑，保存失败不应阻断正常代理。

路由决策日志会输出 `trace_dir` 字段，值为 Trace 目录名，用于从日志行唯一定位对应的 Trace 目录：

```text
route logical model selected_tier=HIGH ... trace_dir=20260716-151230.123456789-000001
upstream response selected_model=... status_code=200 trace_dir=20260716-151230.123456789-000001
```

完整字段说明见 `docs/design.md`。

## 版本管理

版本号在根目录 `VERSION` 文件：

```text
0.1.0
```

构建时自动读取，发布新版本只需修改此文件。

## 当前限制

- 只支持 Chat Completions
- 不持久化会话状态
- Admin 接口无认证
- 不支持不同模型使用不同 API Key
