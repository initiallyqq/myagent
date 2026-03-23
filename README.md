# MyAgent — 旅伴匹配 Agentic Workflow

> **核心思路**：大模型降级为极其廉价的非结构化数据提取器，PostgreSQL 承担核心的业务与向量计算，Redis 挡住并发洪峰，最后用微信生态完成零成本的异步触达。

---

## 架构全景

```
客户端 / 小程序
     │
     ▼
API 网关 & 限流 (Redis 滑动窗口)
     │
     ├─ 命中语义缓存 ──────────────────────► SSE 直接返回
     │
     ▼
意图提取层
  ├─ LLM JSON Mode (3s 超时熔断)
  └─ 正则兜底提取 (目的地词典 + 性别/预算/月份)
     │
     ▼
混合检索核心 (PostgreSQL + pgvector)
  精确过滤 (gender / destinations / budget)
  + 余弦向量排序 (embedding <=>)
     │
     ├─ 有结果 ──► 写 Redis 缓存 ──► SSE 返回话术模板
     │
     └─ 无结果 ──► 一级降级 (摘除 budget/time)
                    └─ 无结果 ──► 二级降级 (目的地扩区域)
                                   └─ 无结果 ──► 意图悬挂 (写 demand_pool)
                                                  └─ 引导用户订阅通知

后台 Cron Job (每 10 分钟)
  扫 demand_pool ──► 向量匹配 ──► 微信订阅消息免费触达
```

---

## 技术栈

| 组件 | 选型 | 理由 |
|---|---|---|
| 业务层 | Go 1.22 / Gin | 高并发，原生 SSE 支持 |
| 大模型 | DeepSeek-V3 / Qwen-Max / Gemini Flash | JSON Mode，每百万 Token 几块钱 |
| 核心存储 | PostgreSQL 15 + pgvector | 一张表搞定标量过滤 + 向量检索 |
| 缓存/限流 | Redis 7 | 语义缓存 TTL=2h，滑动窗口限流 |
| 消息触达 | 微信小程序订阅消息 | 免费，高到达率 |
| 容器编排 | Docker Compose | 单机部署，无微服务 |

---

## 项目结构

```
myagent/
├── cmd/server/main.go              # 入口：依赖注入、路由、优雅关机
├── internal/
│   ├── config/config.go            # YAML 配置加载（单例）
│   ├── handler/
│   │   ├── search.go               # POST /api/v1/search (SSE 全链路)
│   │   ├── user.go                 # POST /api/v1/user/register
│   │   └── subscribe.go            # POST /api/v1/subscribe
│   ├── middleware/
│   │   ├── ratelimit.go            # Redis 滑动窗口限流
│   │   └── timeout.go              # 请求级超时中间件
│   ├── service/
│   │   ├── intent.go               # LLM 提取 → 防腐校验 → 正则合并
│   │   ├── search.go               # 三级降级检索 + 意图悬挂
│   │   ├── cache.go                # SHA256 语义缓存 Key
│   │   └── notify.go               # 微信 Access Token 管理 + 消息发送
│   ├── model/
│   │   ├── user.go                 # 用户领域模型
│   │   ├── demand.go               # 需求池模型
│   │   └── intent.go               # Intent struct + Validate()
│   ├── repo/
│   │   ├── user_repo.go            # Upsert + HybridSearch (精确+向量)
│   │   └── demand_repo.go          # CRUD + 批量余弦相似度查询
│   ├── llm/
│   │   ├── client.go               # HTTP 调用 LLM，3s 超时，JSON Mode
│   │   └── fallback_regex.go       # 正则兜底（目的地词典、性别、预算）
│   └── cron/
│       └── match_job.go            # 定时反向匹配 + 新用户触发唤醒
├── pkg/
│   ├── sse/writer.go               # SSE 帧封装 (Send / Done)
│   └── template/reply.go           # Sprintf 话术模板，禁止 LLM 生成回复
├── migrations/
│   ├── 001_create_users.sql        # users 表 + pgvector + 触发器
│   ├── 002_create_demand_pool.sql  # demand_pool 表
│   └── 003_create_indexes.sql      # GIN + HNSW 索引
├── config/config.yaml              # 配置模板
├── docker-compose.yaml
└── Dockerfile
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

编辑 `config/config.yaml`，填入以下必填项：

```yaml
llm:
  api_key: "YOUR_DEEPSEEK_API_KEY"

wechat:
  app_id: "YOUR_WECHAT_APP_ID"
  app_secret: "YOUR_WECHAT_APP_SECRET"
  subscribe_template_id: "YOUR_TEMPLATE_ID"
```

### 2. 启动（全栈容器化）

```bash
docker-compose up --build
```

服务将在 `http://localhost:8080` 启动，PostgreSQL 和 Redis 随之自动初始化（Migration SQL 由 Docker 自动执行）。

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

`bio` 字段会自动生成 embedding 向量存入数据库。

---

### `POST /api/v1/subscribe` — 授权订阅通知

```bash
curl -X POST http://localhost:8080/api/v1/subscribe \
  -H "Content-Type: application/json" \
  -d '{"openid": "wx_openid_xxx", "template_id": "your_template_id"}'
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

---

## 落地红线

1. **LLM 超时熔断**：所有 LLM 调用强制 `Timeout = 3s`，超时立即走正则兜底，不可用比不智能更可怕。

2. **HNSW 索引**：用户量突破 10 万时，必须对 `embedding` 字段建 HNSW 索引（`003_create_indexes.sql` 已包含），否则向量检索会拖垮 CPU。

3. **禁止 LLM 生成回复**：所有面向用户的文本均由 `pkg/template/reply.go` 的 `fmt.Sprintf` 模板拼接，杜绝幻觉和额外 Token 费用。

4. **防腐层强制校验**：LLM 返回的 JSON 必须通过 `model.Intent.Validate()` 校验，任何 Markdown 符号或格式错误在此层清洗，绝不污染 SQL。

---

## 降级策略

```
用户请求
   │
   ▼
严格搜索: gender + destinations + budget + 向量排序
   │ 无结果
   ▼
宽松搜索: 摘除 budget / 时间约束
   │ 无结果
   ▼
区域扩展: 西藏→大西北, 大理→云南, 稻城→四川 ...
   │ 无结果
   ▼
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
