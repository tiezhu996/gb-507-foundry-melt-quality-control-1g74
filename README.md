
# 铸造炉次成分与质量判定

面向铸造工厂的炉次、取样、化验与质量决定协同平台。项目采用前后端分离和明确的领域分层，重点保证状态迁移、RBAC、审计日志、请求追踪与限流在各层保持一致。

## Docker Compose 快速启动

```bash
cp .env.example .env
docker compose up -d --build
docker compose ps
```

- Web 工作台：http://127.0.0.1:18507
- 后端健康检查：http://127.0.0.1:19507/healthz
- 后端 API：http://127.0.0.1:19507/api
- 演示管理员：`admin` / `Admin123!`（仅限本地演示，生产环境必须更换）

停止并清理本项目容器与数据卷：

```bash
docker compose down -v --remove-orphans
```

## 主要功能

| 业务模块 | 后端实体 | API 前缀 | 状态流 |
|---|---|---|---|
| 炉台 | `Furnace` | `/api/furnaces` | available, charging, maintenance, locked |
| 炉次 | `Heat` | `/api/heats` | charged, melting, sampling, hold, accepted, rejected |
| 化验样本 | `ChemicalSample` | `/api/samples` | collected, testing, verified, rejected |
| 质量决定 | `QualityDecision` | `/api/decisions` | draft, accept, remelt, scrap |

### 炉次返炉承接闭环

质量负责人把判定签发为**返炉（remelt）**时，必须通过专用闭环接口
`POST /api/decisions/:id/remelt` 一次提交完成，普通 `transition` 接口不接受 remelt：

1. 校验判定仍为 draft、原炉次仍在 quality hold、终态样本归属一致；
2. 只能选择**当前 available** 且满足原牌号（`supportedAlloys`）、装料量
   （`capacityTonnes`）和目标温度（`maxTemperatureC`）的承接炉台；候选清单由
   `GET /api/heats/remelt-furnaces?heatCode=` 返回，不合规炉台附具体原因；
3. 同一事务内：原炉次置 `rejected`、判定置 `remelt` 并记录承接地炉台与返炉炉次、
   生成继承全部冻结成分规格的 `charged` 返炉炉次、承接炉台原子占用为 `charging`；
4. 无合规炉台、重复提交或并发冲突均整体回滚，不改动炉次、炉台或判定记录，并返回
   具体原因（业务原因 422、乐观锁/并发冲突 409）。

炉次页显示「原炉次 ↔ 返炉炉次」关系与承接地炉台，质量判定页显示关联结果，刷新后可回读。
接收（accept）、报废（scrap）和普通炉次流程保持不变。

- JWT 登录和 viewer/operator/reviewer/admin 四级 RBAC。
- 所有状态变化使用乐观锁并写入不可覆盖的审计日志。
- 请求 ID、结构化日志、全局错误映射和 Redis 分布式限流。
- 提供脱敏运行配置、当前会话、审计汇总和单实体审计历史接口。
- 业务工作台支持查询、新建、状态推进、风险标识及操作审计查看。

## 技术栈

| 层次 | 技术 |
|---|---|
| 前端 | React 18 + TypeScript + Vite + Material UI |
| 后端 | Go 1.22 + Gin + GORM |
| 数据 | PostgreSQL + Redis |
| 部署 | Docker Compose + Nginx |

## 本地开发

后端可使用 SQLite 开发模式，不需要先启动数据库：

```bash
cd backend
go mod download
DATABASE_DRIVER=sqlite DATABASE_DSN=local.db REDIS_ADDR='' \
JWT_SECRET=local-development-secret PORT=8080 go run ./cmd/server
```

前端开发服务器：

```bash
cd frontend
npm install
npm run dev
```

质量检查：

```bash
cd backend && go test ./... && go build ./...
cd ../frontend && npm run typecheck && npm run build
cd .. && docker compose config --quiet
```

也可以从项目根目录执行 `./scripts/validate.sh`，脚本会构建、启动、检查健康接口和鉴权 API，并在结束时关闭容器。

## 目录结构

```text
.
├── backend/
│   ├── cmd/server/                 # 服务入口与优雅退出
│   └── internal/
│       ├── config/                 # 环境配置
│       ├── constants/              # 状态枚举与迁移图
│       ├── database/               # 连接、迁移与演示数据
│       ├── dto/                    # 输入契约
│       ├── handler/                # HTTP 接口
│       ├── middleware/             # JWT、追踪、限流
│       ├── model/                  # GORM 实体
│       ├── repository/             # 持久化边界
│       ├── router/                 # 路由装配
│       ├── service/                # 业务规则与审计
│       └── util/                   # 统一 HTTP 响应
├── frontend/src/
│   ├── api/                        # 按实体拆分的 API
│   ├── components/common/          # 共享业务组件
│   ├── hooks/                      # 认证与分页 hooks
│   ├── pages/                      # 五个路由页面
│   ├── router/                     # 路由配置
│   ├── stores/                     # 按实体拆分的状态仓库
│   ├── types/                      # 共享类型与枚举
│   └── utils/                      # 格式化与状态工具
├── docker-compose.yml
└── runtime_smoke.json
```

## 共享枚举位置

| 枚举 | 值 | 前后端出现位置 |
|---|---|---|
| `HeatState` | `charged, melting, sampling, hold, accepted, rejected` | `backend/internal/constants/status.go`、`frontend/src/types/status.ts` |
| `DecisionType` | `accept, remelt, scrap` | `backend/internal/constants/status.go`、`frontend/src/types/status.ts` |

每个实体自己的完整迁移图同样位于 `backend/internal/constants/status.go`；页面使用的状态列表位于 `frontend/src/types/status.ts`。修改状态时必须同步两处并更新对应服务测试。

## 环境变量

| 变量 | 说明 |
|---|---|
| `COMPOSE_PROJECT_NAME` | 固定英文 Compose 项目名，支持中文父目录 |
| `DB_NAME/DB_USER/DB_PASSWORD` | 数据库名称与业务账号 |
| `DB_ROOT_PASSWORD` | MySQL 管理员密码（PostgreSQL 项目保留统一模板字段） |
| `JWT_SECRET` | JWT 签名密钥，生产环境必须替换 |
| `FRONTEND_PORT/BACKEND_PORT/DB_PORT` | 宿主机端口映射 |
| `REDIS_PORT` | Redis 宿主机端口 |
| `MINIO_*` | 证据对象存储配置（启用 MinIO 的项目） |

## API 使用示例

```bash
token=$(curl -sS -X POST http://127.0.0.1:19507/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin123!"}' | jq -r '.data.token')

curl -sS http://127.0.0.1:19507/api/overview \
  -H "Authorization: Bearer $token"
```

## License

MIT
