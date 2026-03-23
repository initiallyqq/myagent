# MyAgent — 旅伴匹配 Agentic Workflow

> **核心思路**：以 ReAct 循环驱动 Agent 自主推理，LLM Function Calling 调度标准化 Skill，mem0 持久记忆注入上下文，MCP 协议对外暴露工具能力，PostgreSQL 承担混合向量检索，Redis 挡住并发洪峰，微信生态完成零成本异步触达。

---

## 架构全景

```
客户端 / 小程序
     │
     ▼
API 网关 & 限流 (Redis 滑动窗口)
     │
     ├─ 命中语义缓存 ──────────────────────────────► SSE 直接返回
     │
     ▼
Agent Orchestrator (ReAct 循环, max_steps=6)
  ├─ Memory 注入 (mem0: 召回用户历史记忆 → 注入 System Prompt)
  ├─ LLM Chat + Function Calling (Reason → 选择 Tool)
  └─ Skill Registry (Act → Execute → Observe → 回写 message history)
        ├─ search_companions   → HybridSearch (精确+向量, 三级降级)
        ├─ suspend_demand      → 意图悬挂写 demand_pool
        ├─ save_memory         → 异步提取并持久化记忆向量
        └─ get_user_profile    → 查询当前用户画像
     │
     ├─ 有最终答案 ──► 异步提取记忆 ──► 写 Redis 缓存 ──► SSE 返回
     │
     └─ 无结果 ──────────────────────────────────► 意图悬挂 + 通知引导

MCP Server (/mcp/tools/list, /mcp/tools/call)
  ↑ 供外部 LLM Client 直接调用同一套 Skill

后台 Cron Job (每 10 分钟)
  扫 demand_pool ──► 向量匹配 ──► 微信订阅消息免费触达
```

---

## 技术栈

| 组件 | 选型 | 理由 |
|---|---|---|
| 业务层 | Go 1.22 / Gin | 高并发，原生 SSE 支持 |
| 大模型 | DeepSeek-V3 / Qwen-Max | Function Calling + JSON Mode |
| Agent 范式 | ReAct (Reason + Act + Observe) | 多轮自主推理，最大 6 步 |
| 记忆层 | mem0 风格持久记忆 | 向量检索历史记忆 + System Prompt 注入 |
| 工具协议 | Model Context Protocol (MCP) | 标准化工具发现与调用 |
| 核心存储 | PostgreSQL 15 + pgvector | 标量过滤 + HNSW 向量检索 |
| 缓存/限流 | Redis 7 | 语义缓存 TTL=2h，滑动窗口限流 |
| 消息触达 | 微信小程序订阅消息 | 免费，高到达率 |
| 容器编排 | Docker Compose | 单机部署 |

---

## 项目结构

按分层架构组织，从接入层到存储层依次向下：

```
myagent/
│
├── cmd/server/main.go                  # 程序入口：依赖注入 / 路由注册 / 优雅关机
│
├── internal/
│   │
│   ├── ── 接入层 ──────────────────────────────────────────
│   ├── handler/
│   │   ├── search.go                   # POST /api/v1/search  → Orchestrator → SSE
│   │   ├── user.go                     # POST /api/v1/user/register
│   │   └── subscribe.go                # POST /api/v1/subscribe
│   ├── middleware/
│   │   ├── ratelimit.go                # Redis 滑动窗口限流
│   │   └── timeout.go                  # 请求级超时中间件
│   │
│   ├── ── Agent 层 ─────────────────────────────────────────
│   ├── agent/
│   │   └── orchestrator.go             # ReAct 循环 (Reason → Act → Observe, max 6步)
│   ├── memory/
│   │   ├── store.go                    # mem0 持久记忆：向量写入 / 语义召回 / 最近记忆
│   │   └── manager.go                  # 记忆提取（异步 LLM 抽取）+ System Prompt 注入
│   ├── mcp/
│   │   ├── tools.go                    # OpenAI 兼容 Tool Schema 定义 (ToolDefs)
│   │   └── server.go                   # MCP HTTP 端点：GET /mcp/tools/list, POST /mcp/tools/call
│   ├── skill/
│   │   ├── registry.go                 # Skill 接口 + Registry（注册 / 调度 / ToolDispatcher）
│   │   ├── search.go                   # SearchSkill     → 三级降级混合检索
│   │   ├── memory_skill.go             # MemorySkill     → 持久化记忆事实
│   │   ├── suspend.go                  # SuspendSkill    → 意图悬挂写 demand_pool
│   │   └── profile.go                  # ProfileSkill    → 查询当前用户画像
│   │
│   ├── ── 业务服务层 ───────────────────────────────────────
│   ├── service/
│   │   ├── intent.go                   # LLM 意图提取 → 防腐校验 → 正则兜底合并
│   │   ├── search.go                   # 三级降级检索主逻辑 + 意图悬挂触发
│   │   ├── cache.go                    # SHA256 语义缓存 Key / Redis TTL 管理
│   │   └── notify.go                   # 微信 Access Token 刷新 + 订阅消息发送
│   ├── cron/
│   │   └── match_job.go                # 定时反向匹配（新用户唤醒 demand_pool）
│   │
│   ├── ── LLM 层 ───────────────────────────────────────────
│   ├── llm/
│   │   ├── client.go                   # HTTP 调用 LLM；支持 JSON Mode + Function Calling
│   │   └── fallback_regex.go           # 正则兜底：目的地词典 / 性别 / 预算解析
│   │
│   ├── ── 数据层 ───────────────────────────────────────────
│   ├── repo/
│   │   ├── user_repo.go                # Upsert / HybridSearch / VectorLiteral()
│   │   └── demand_repo.go              # CRUD + 批量余弦相似度查询
│   ├── model/
│   │   ├── user.go                     # 用户领域模型
│   │   ├── demand.go                   # 需求池模型
│   │   └── intent.go                   # Intent struct + Validate()
│   │
│   └── config/config.go                # YAML 配置加载（单例）
│
├── pkg/                                # 无业务依赖的通用工具
│   ├── sse/writer.go                   # SSE 帧封装 (Send / Done)
│   └── template/reply.go               # Sprintf 话术模板（禁止 LLM 直接生成回复）
│
├── migrations/                         # 按序执行的数据库迁移
│   ├── 001_create_users.sql            # users 表 + pgvector + 更新触发器
│   ├── 002_create_demand_pool.sql      # demand_pool 表
│   ├── 003_create_indexes.sql          # GIN 索引（destinations）+ HNSW（embedding）
│   └── 004_create_memories.sql         # memories 表（mem0）+ HNSW 向量索引
│
├── config/config.yaml                  # 配置模板（llm / db / redis / wechat / agent）
├── docker-compose.yaml                 # 一键启动：app + postgres + redis
└── Dockerfile
```

---

## Agent 核心设计

### ReAct 循环 (orchestrator.go)

```
用户输入
   │
   ▼
注入 Memory 上下文 → System Prompt
   │
   ▼ Loop (max_react_steps = 6)
LLM Chat(messages, tools)
   │
   ├─ 返回 tool_calls ──► Skill Registry Dispatch ──► Observation 写回 messages
   │
   └─ 返回 content ────► 最终答案，退出循环
   │
   ▼
异步：ExtractAndSave(记忆提取)
构建 SearchReply 返回
```

### Function Calling & Skill

LLM 通过 OpenAI 兼容的 `tools` 参数声明可用工具，返回 `tool_calls` 时 Agent 自动调度对应 Skill：

| Tool 名称 | Skill | 功能 |
|---|---|---|
| `search_companions` | SearchSkill | 三级降级混合检索旅伴 |
| `suspend_demand` | SuspendSkill | 无结果时悬挂意图到 demand_pool |
| `save_memory` | MemorySkill | 持久化用户偏好/事实到 memories |
| `get_user_profile` | ProfileSkill | 获取当前用户画像（目的地/标签/预算）|

### mem0 持久记忆

```
用户对话
   │
   ▼
Manager.ExtractAndSave()          ← 异步，不阻塞主流程
  LLM 提取关键事实 (JSON)
  生成 embedding 向量
  写入 memories 表
   │
   ▼ 下次请求
Manager.RetrieveContext()
  向量召回 TopK 相关记忆
  格式化注入 System Prompt
```

### MCP Server

对外暴露标准化工具端点，任何兼容 MCP 的 LLM 客户端均可直接调用：

```bash
# 列出所有工具
GET /mcp/tools/list

# 调用工具
POST /mcp/tools/call
{"name": "search_companions", "arguments": {"query": "五一西藏", "gender": "F"}}
```

---

## 快速启动

### 前置依赖

- Docker & Docker Compose
- Go 1.22+（本地开发）

### 1. 克隆并配置

```bash
git clone git@github.com:initiallyqq/myagent.git
cd myagent
```

编辑 `config/config.yaml`，填入必填项：

```yaml
llm:
  api_key: "YOUR_DEEPSEEK_API_KEY"

wechat:
  app_id: "YOUR_WECHAT_APP_ID"
  app_secret: "YOUR_WECHAT_APP_SECRET"
  subscribe_template_id: "YOUR_TEMPLATE_ID"

agent:
  max_react_steps: 6
  memory_top_k: 5
```

### 2. 启动（全栈容器化）

```bash
docker-compose up --build
```

服务将在 `http://localhost:8080` 启动，PostgreSQL 和 Redis 随之自动初始化。

### 3. 本地开发模式

```bash
# 只启动基础设施
docker-compose up -d postgres redis

# 本地运行 Go 服务
go run ./cmd/server
```

---

## API 接口

### `POST /api/v1/search` — 主搜索（SSE 流式）

```bash
curl -X POST http://localhost:8080/api/v1/search \
  -H "Content-Type: application/json" \
  -H "X-Openid: wx_user_openid" \
  -d '{"query": "五一去西藏，找个E人姐妹"}'
```

响应为 SSE 流：

```
data: {"message":"为你精准匹配到 3 位旅伴：\n1. 小雨 | MBTI: ENFP | 去向: 西藏 | 预算: 8000 元","users":[...],"done":true}

data: [DONE]
```

---

### `POST /api/v1/user/register` — 用户注册/画像更新

```bash
curl -X POST http://localhost:8080/api/v1/user/register \
  -H "Content-Type: application/json" \
  -d '{
    "openid": "wx_openid_xxx",
    "nickname": "小雨",
    "gender": "F",
    "tags": {"mbti": "ENFP", "hobby": ["摄影", "徒步"]},
    "destinations": ["西藏", "云南"],
    "budget_min": 3000,
    "budget_max": 8000,
    "bio": "热爱户外探索，喜欢和有趣的人一起旅行"
  }'
```

`bio` 字段自动生成 embedding 向量存入数据库。

---

### `POST /api/v1/subscribe` — 授权订阅通知

```bash
curl -X POST http://localhost:8080/api/v1/subscribe \
  -H "Content-Type: application/json" \
  -d '{"openid": "wx_openid_xxx", "template_id": "your_template_id"}'
```

---

### `GET /mcp/tools/list` — 列出 MCP 工具

```bash
curl http://localhost:8080/mcp/tools/list
```

### `POST /mcp/tools/call` — 调用 MCP 工具

```bash
curl -X POST http://localhost:8080/mcp/tools/call \
  -H "Content-Type: application/json" \
  -d '{"name": "search_companions", "arguments": {"query": "五一西藏", "gender": "F"}}'
```

---

### `GET /health` — 健康检查

```bash
curl http://localhost:8080/health
# {"status":"ok","time":"2026-03-23T10:00:00+08:00"}
```

---

## 数据库 Schema

### users 表

| 字段 | 类型 | 说明 |
|---|---|---|
| `openid` | varchar(64) | 微信 OpenID，唯一键 |
| `gender` | char(1) | M / F / X |
| `tags` | jsonb | `{"mbti":"ENFP","hobby":["摄影"]}` |
| `destinations` | text[] | 目的地数组，GIN 索引 |
| `budget_min/max` | int | 预算范围 |
| `embedding` | vector(1024) | bio 语义向量，HNSW 索引 |

### demand_pool 表

| 字段 | 类型 | 说明 |
|---|---|---|
| `requester_id` | bigint | 关联 users.id |
| `intent_json` | jsonb | 原始意图 JSON |
| `intent_vector` | vector(1024) | 意图向量，用于反向匹配 |
| `status` | varchar | pending / matched / expired |
| `expires_at` | timestamptz | 默认 30 天后过期 |

### memories 表（mem0）

| 字段 | 类型 | 说明 |
|---|---|---|
| `user_id` | bigint | 关联 users.id |
| `content` | text | 记忆文本（提取的事实） |
| `embedding` | vector(1024) | 记忆语义向量，HNSW 索引 |
| `created_at` | timestamptz | 记忆时间戳 |

---

## 落地红线

1. **LLM 超时熔断**：所有 LLM 调用强制 `Timeout = 3s`，超时立即走正则兜底，不可用比不智能更可怕。

2. **ReAct 步数上限**：`max_react_steps = 6`，防止 Agent 陷入无限推理循环，超限直接返回当前最佳观察。

3. **HNSW 索引**：users 和 memories 表的 embedding 字段均已建 HNSW 索引，向量检索不拖垮 CPU。

4. **禁止 LLM 生成回复**：所有面向用户的文本均由 `pkg/template/reply.go` 的 `fmt.Sprintf` 模板拼接，杜绝幻觉。

5. **防腐层强制校验**：LLM 返回的 JSON 必须通过 `model.Intent.Validate()` 校验，任何格式错误在此清洗，绝不污染 SQL。

6. **记忆提取异步化**：`ExtractAndSave()` 在 goroutine 中运行，失败只记日志，不影响主流程响应。

---

## 降级策略

```
用户请求 → Agent ReAct 循环
   │
   ▼ search_companions (严格)
严格搜索: gender + destinations + budget + 向量排序
   │ 无结果 → LLM 重新推理
   ▼ search_companions (宽松)
宽松搜索: 摘除 budget / 时间约束
   │ 无结果 → LLM 重新推理
   ▼ search_companions (扩区域)
区域扩展: 西藏→大西北, 大理→云南, 稻城→四川 ...
   │ 无结果 → LLM 决策调用 suspend_demand
   ▼ suspend_demand
意图悬挂: 写入 demand_pool，引导用户订阅通知
   │
   ▼ (Cron Job 每 10 分钟)
反向匹配: 新用户画像向量 <=> demand intent_vector
   │ 相似度 > 0.75
   ▼
微信订阅消息免费触达
```

---

## License

MIT
