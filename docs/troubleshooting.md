# 故障排查指南

本文档记录 Smart Coder Switch 运行中遇到的典型问题、诊断步骤和解决方案。

## 目录

- [日志位置](#日志位置)
- [错误分类速查](#错误分类速查)
- [常见问题](#常见问题)
  - [P1: upstream request failed + 502 + context canceled](#p1-upstream-request-failed--502--context-canceled)
  - [P2: upstream request failed + 400 + Error from provider](#p2-upstream-request-failed--400--error-from-provider)
- [诊断工具](#诊断工具)
- [故障记录模板](#故障记录模板)

---

## 日志位置

```
./log/
├── smart-coder-switch.log   # 主运行日志
├── smart-coder-switch.pid   # 当前进程 PID
└── traces/                   # 请求跟踪文件
    └── YYYYMMDD-HHMMSS.nanosec-seq/
        ├── request.json      # 原始请求体
        └── decision.json     # 路由决策详情
```

关键日志字段：
- `trace_dir`: 用于关联同一请求的多条日志
- `error_kind`: 错误来源分类（client_cancel / upstream_cancel / upstream_timeout / upstream_error）
- `status_code`: 上游 HTTP 状态码
- `body_preview`: 非 2xx 响应体摘要（前 4096 字节）

---

## 错误分类速查

| 日志特征                                           | 错误类型     | 根因位置     | 是否代理问题 |
| -------------------------------------------------- | ------------ | ------------ | ------------ |
| `upstream request failed` + `error_kind=client_cancel` | 客户端取消   | OpenCode → 代理 | ❌ 否        |
| `upstream request failed` + `error_kind=upstream_cancel` | 上游连接中断 | 代理 → 上游  | ⚠️ 可能      |
| `upstream request failed` + `error_kind=upstream_timeout` | 上游超时     | 代理 → 上游  | ⚠️ 可能      |
| `upstream non-2xx response` + `status_code=400`    | 上游拒绝请求 | 上游 provider | ❌ 否        |
| `upstream non-2xx response` + `status_code=502/503` | 上游服务错误 | 上游服务     | ❌ 否        |

---

## 常见问题

### P1: upstream request failed + 502 + context canceled

**现象**：
- OpenCode 报错：`Upstream request failed`
- 代理日志：`upstream request failed` + `error_kind=client_cancel` 或 `upstream_cancel`
- HTTP 状态码：502 Bad Gateway

**根因分析**：

```
错误来源判断：
  error_kind=client_cancel
  && request_context_error=context canceled
  → 入站请求 context 先被取消（OpenCode 侧取消）

  error_kind=upstream_cancel
  && request_context_error 为空
  → 入站 context 正常，但上游连接/传输中断
```

**诊断步骤**：

1. 查看日志中的 `error_kind` 和 `request_context_error`：
   ```bash
   grep -E "upstream request failed|error_kind" log/smart-coder-switch.log | tail -20
   ```

2. 关联对应 trace：
   - 日志中的 `trace_dir` 字段值
   - 读取 `log/traces/<trace_dir>/request.json` 查看请求体
   - 读取 `decision.json` 查看路由决策

3. 判断方向：
   - 如果 `client_cancel`：OpenCode 在等待过程中取消请求（可能是 OpenCode 超时或用户手动停止）
   - 如果 `upstream_cancel`：代理到上游的连接不稳定

**解决方案**：

- `client_cancel`：检查 OpenCode 侧超时配置，或用户行为
- `upstream_cancel`：考虑增加上游重试（需要配置 `retry-count`，当前版本未实现）

**变更记录**：
- 2026-07-17：增强 `ErrorHandler` 日志，增加 `error_kind` / `request_context_error` 字段，区分取消方向

---

### P2: upstream request failed + 400 + Error from provider

**现象**：
- OpenCode 报错：`Upstream request failed`
- 代理日志：
  ```
  level=WARN msg="upstream non-2xx response" status_code=400
  body_preview="{\"error\":{\"message\":\"Error from provider (Console Go): Upstream request failed\"...}}"
  ```
- HTTP 状态码：400 Bad Request

**根因分析**：

这不是代理问题。代理成功将请求转发到上游 `one-api`，但上游返回 400。

经过直接上游矩阵测试，**精确根因是**：

> **`deepseek-v4-flash` (Console Go) 验证 `tool_call_id` 是否为自己生成的。当请求最后一条 tool 消息的 `tool_call_id` 不是 deepseek 自己生成的（例如来自 gpt-5.6-terra），则拒绝请求。**

换句话说：
```
deepseek 生成的 tool_call_id 格式：call_00_<30位字符>   (如 call_00_ET_EjUfMBjoYGuFXEIHfq3J6898)
gpt-5.6-terra 生成的 tool_call_id 格式：call_<24位字符>   (如 call_KZQBjvAeiexzeLrMBoMOvgWW)
```

当 Smart Coder Switch 发生模型切换时：
1. 上一轮 `HIGH/DIRECT` 路由到 `gpt-5.6-terra` → gpt 生成 `call_*` 格式 ID
2. 下一轮随机路由回到 `deepseek-v4-flash` → deepseek 不识别这个 ID → **400**

**证据链**（所有测试直连上游，排除代理干扰）：

| 测试 | 操作 | 结果 |
|------|------|------|
| 把 OK trace 的最后一对 tool_call_id 改为任意值 | 仅改最后一位字符 | **400** |
| 把 OK trace 的中间对的 tool_call_id 改为任意值 | 中间对不改 | **200** |
| 把 OK trace 的最后一对 tool_call_id 改为 gpt 生成的 ID | `call_KZQBjvAeiexzeLrMBoMOvgWW` | **400** |
| 不改 ID，只改 arguments 为占位符 | `{"filePath":"/tmp/x","oldString":"a","newString":"b"}` | **200** |
| 不改 ID，只改 tool content 为 "ok" | `"ok"` | **200** |
| 不改 ID，末尾追加 user 消息 | 改末尾为 user | **200** |

**关键结论**：
- 不是"最后一条消息为 tool"的问题——纯 deepseek 的 tool 链没问题
- 不是"连续 tool call"的问题——deepseek 可以处理大量连续 tool call
- 不是"arguments/消息内容"的问题——改内容不影响
- **精确定位**：`tool_call_id` 最后一条消息的 ID 必须匹配 deepseek 自己生成的格式

**诊断步骤**：

1. 找到错误日志的 `trace_dir`：
   ```bash
   grep "upstream non-2xx response" log/smart-coder-switch.log | tail -5
   ```

2. 查看 trace 的上一条请求模型：
   ```bash
   # 找到这个 trace_dir 之前的一个 upstream response
   grep -B 1 "trace_dir=YYYYMMDD-HHMMSS.seq-xxx" log/smart-coder-switch.log | head -2
   ```

3. 确认是否**连续两次请求用了不同模型**：
   ```bash
   # 提取完所有 deepseek 400 前面的模型
   python3 - <<'PY'
   import re
   from pathlib import Path
   log = Path('log/smart-coder-switch.log').read_text().splitlines()
   models = [(re.search(r'trace_dir=(\S+)', l).group(1),
              re.search(r'selected_model=(\S+)', l).group(1),
              re.search(r'(\d{3})', l.split('status_code=')[1]).group(1) if 'status_code=' in l else None)
             for l in log if 'selected_model=' in l]
   for i, (trace, model, status) in enumerate(models):
       if model and 'deepseek' in model and status == '400' and i > 0:
           prev = models[i-1]
           print(f'400 at {trace}, prev was {prev[0]} -> {prev[1]} {prev[2]}')
   PY
   ```

4. 直接测试上游（绕过代理）：
   ```bash
   curl -X POST https://one-api.ffpy.site/v1/chat/completions \
     -H "Authorization: Bearer $API_KEY" \
     -H "Content-Type: application/json" \
     -d '{"model":"deepseek-v4-flash","stream":true,"messages":[{"role":"user","content":"test"}]}'
   ```

**解决方案**：

- **方案 A（推荐）**：检测到消息中 `tool_calls` 或 `role=tool` 时，固定使用同一模型，避免在 tool-call 上下文中切换模型

- **方案 B（临时规避）**：修改配置，将 `deepseek-v4-flash` 不在 tool 上下文使用
  ```yaml
  models:
    smart-coder:
      low-model: gpt-5.6-terra        # 换成稳定模型
      medium-model: gpt-5.6-terra
  ```

- **方案 C**：在路由层增加检测，如果消息中存在 `tool_calls` 或最后一条是 `tool`，不走 deepseek

**变更记录**：
- 2026-07-17：增加 `ModifyResponse` 日志，记录非 2xx 响应体摘要
- 2026-07-17：通过直接上游测试确认根因为 **deepseek 验证 tool_call_id 必须为自己生成**
- 2026-07-17：直连上游矩阵测试（15+ 组），精确定位触发条件

---

## 诊断工具

### 日志过滤脚本

```bash
# 查看最近的错误
python3 - <<'PY'
from pathlib import Path
p = Path('log/smart-coder-switch.log')
lines = p.read_text(errors='replace').splitlines()
patterns = ['upstream request failed', 'upstream non-2xx response', 'error_kind', 'status_code=']
for i, line in enumerate(lines, 1):
    if any(x in line for x in patterns):
        print(i, line[:500])
PY
```

### 直接上游测试

用于绕过代理验证上游行为：

```python
import json, urllib.request, os

url = 'https://one-api.ffpy.site/v1/chat/completions'
key = os.environ['TEST_ONEAPI_KEY']

payload = {
    'model': 'deepseek-v4-flash',
    'messages': [{'role': 'user', 'content': 'ping'}],
}

req = urllib.request.Request(
    url,
    data=json.dumps(payload).encode(),
    headers={
        'Authorization': f'Bearer {key}',
        'Content-Type': 'application/json',
    }
)

try:
    with urllib.request.urlopen(req, timeout=60) as resp:
        print('status:', resp.status)
        print('body:', resp.read(1000).decode())
except urllib.error.HTTPError as e:
    print('status:', e.code)
    print('body:', e.read(4000).decode())
```

验证 Responses 协议时，可直接调用上游 `/v1/responses`：

```python
import json, urllib.request, os

url = 'https://one-api.ffpy.site/v1/responses'
key = os.environ['TEST_ONEAPI_KEY']

payload = {
    'model': 'deepseek-v4-flash',
    'input': 'ping',
}

req = urllib.request.Request(
    url,
    data=json.dumps(payload).encode(),
    headers={
        'Authorization': f'Bearer {key}',
        'Content-Type': 'application/json',
    }
)

try:
    with urllib.request.urlopen(req, timeout=60) as resp:
        print('status:', resp.status)
        print('body:', resp.read(1000).decode())
except urllib.error.HTTPError as e:
    print('status:', e.code)
    print('body:', e.read(4000).decode())
```

---

## 故障记录模板

每次解决新问题后，按以下格式补充：

```markdown
### PX: <简短标题>

**现象**：
- OpenCode 报错：...
- 代理日志：...
- HTTP 状态码：...

**根因分析**：
<详细分析>

**诊断步骤**：
1. ...
2. ...

**解决方案**：
- 方案 A：...
- 方案 B：...

**变更记录**：
- YYYY-MM-DD：...
```

---

## 维护说明

1. **添加新问题**：按模板在"常见问题"章节追加
2. **更新解决方案**：如果发现更好的方案，补充到对应问题
3. **版本关联**：涉及代码变更时，记录 commit hash 和变更说明
4. **定期清理**：过时的问题（如已在上游修复）标记为"已解决"

---

## 附录

### 相关文档

- [设计文档](./design.md)
- [配置说明](../README.md)

### 联系方式

- 问题反馈：<项目 issue 地址>
- 紧急联系：<维护者联系方式>