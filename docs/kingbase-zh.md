# Faucet × KingbaseES（人大金仓）使用指南

本文档介绍如何将 Faucet 与人大金仓 KingbaseES 数据库配合使用，把金仓库中的表一键变成安全的 REST API。

- **支持版本**：KingbaseES V8R6（基于 PostgreSQL 12 内核）
- **驱动**：官方 gokb 驱动（以本地 vendored 形式内置于 `third_party/gokb/`，无需额外安装）
- **兼容前提**：数据库实例需运行在 **PG 兼容模式**（`DB_MODE=pg`，详见下文）

---

## 1. 准备 KingbaseES 数据库

### 1.1 已有实例：确认兼容模式

Faucet 走 PostgreSQL 方言，要求金仓实例运行在 PG 兼容模式下。用 ksql 确认：

```sql
SHOW database_mode;
```

- 输出 `pg` → 可以直接使用
- 输出 `oracle` / `mysql` → 该实例为其他兼容模式，方言与类型系统不同，**不在支持范围内**（Oracle 模式下 `initdb` 的 `--enable-ci` 大小写不敏感等行为会导致 SQL 语义差异）

### 1.2 Docker 快速启动（测试/体验）

```bash
docker run -d --name faucet-kb \
  -e DB_MODE=pg \
  -e ENABLE_CI=no \
  -e DB_USER=system \
  -e DB_PASSWORD=你的密码 \
  -p 54321:54321 \
  kingbase_v008r006c009b0014_single_x86:v1
```

注意两个关键参数：

| 参数 | 说明 |
|---|---|
| `DB_MODE=pg` | **必须**。镜像默认是 oracle 模式，必须显式改为 pg |
| `ENABLE_CI=no` | **必须**（搭配 pg 模式）。镜像默认 `ENABLE_CI=yes` 会在 pg 模式下导致 initdb 报错 `case-insensitive should only be enabled in oracle mode` |

启动后约 30~60 秒完成初始化，默认监听 **54321** 端口。

---

## 2. 启动 Faucet 并添加服务

### 2.1 启动与初始化

```bash
# 启动服务（默认监听 8080，数据目录 ~/.faucet）
faucet serve

# 创建管理员账户
faucet admin create --email admin@example.com --password changeme123
```

### 2.2 添加 Kingbase 服务

```bash
faucet db add --name kb \
  --driver kingbase \
  --dsn "kingbase://system:你的密码@127.0.0.1:54321/test?sslmode=disable"
```

验证连接：

```bash
faucet db test kb
# Testing connection "kb" (driver=kingbase)...
# Connection successful.
```

> **注意**：`serve` 在启动时加载服务列表，之后通过 `db add` 新增的服务需要重启生效：
> ```bash
> faucet stop && faucet serve
> ```

### 2.3 通过管理界面添加（可选）

打开 `http://localhost:8080` 进入管理界面 → **Services** → 新建服务，Driver 选择 **Kingbase**，按占位符提示填写 DSN 即可，效果与 CLI 等价。

---

## 3. DSN 格式说明

| 格式 | 示例 | 说明 |
|---|---|---|
| URL 风格（推荐） | `kingbase://user:pass@host:54321/dbname?sslmode=disable` | 默认端口 **54321**（注意不是 PG 的 5432） |
| PG 风格自动兼容 | `postgres://user:pass@host:54321/dbname` | Faucet 会自动把 `postgres://` 前缀归一化为 `kingbase://`，习惯写 PG DSN 的用户无感切换 |
| 键值风格 | `user=system password=xxx host=127.0.0.1 port=54321 dbname=test` | lib/pq 风格，同样支持 |

**sslmode 行为**（与 PostgreSQL 驱动的差异）：

- gokb 驱动的原生默认值是 `sslmode=require`（pgx 是 `prefer`）。为保持与 postgres 连接器一致的开箱体验，**Faucet 会在 DSN 未指定 sslmode 时自动补 `sslmode=disable`**
- 使用 SSL 的环境请显式写 `?sslmode=verify-full` 等值（gokb 支持 `require` / `verify-ca` / `verify-full` / `disable`，**不支持** `prefer` / `allow`）

其他要点：

- DSN 中的特殊字符密码（`@`、`#`、`%` 等）会被自动转义，无需手动 URL 编码
- 默认 schema 为 `public`，可用 `--schema` 参数（或服务配置）覆盖

---

## 4. REST API 使用

### 4.1 创建访问凭据

```bash
# 只读角色 + API 密钥
faucet role create --name default --verbs GET
faucet key create --role default

# 读写角色（按需）
faucet role create --name writer --verbs GET,POST,PUT,DELETE
faucet key create --role writer
```

密钥只在创建时显示一次，请立即保存。

### 4.2 查询数据（GET）

```bash
curl -H "X-API-Key: faucet_YOUR_KEY" \
  "http://localhost:8080/api/v1/kb/_table/kb_integration?limit=2"
```

```json
{
  "resource": [
    {"age": 30, "id": 1, "name": "alice"},
    {"age": 25, "id": 2, "name": "bob"}
  ],
  "meta": {"count": 2, "limit": 2, "offset": 0, "took_ms": 1.434}
}
```

带过滤与字段选择（SQL 风格 filter，注意 URL 编码）：

```bash
curl -H "X-API-Key: faucet_YOUR_KEY" \
  "http://localhost:8080/api/v1/kb/_table/kb_integration?filter=age>25&select=name,age"
```

### 4.3 插入数据（POST）

```bash
curl -X POST -H "X-API-Key: faucet_YOUR_WRITER_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"dave","age":35}' \
  "http://localhost:8080/api/v1/kb/_table/kb_integration"
```

```json
{
  "resource": [{"age": 35, "id": 5, "name": "dave"}],
  "meta": {"count": 1, ...}
}
```

自增主键通过 `RETURNING` 自动回填。

### 4.4 删除数据（DELETE）

字符串值使用**单引号**（SQL 语法），并对特殊字符做 URL 编码：

```bash
# filter=name='dave' 编码后：
curl -X DELETE -H "X-API-Key: faucet_YOUR_WRITER_KEY" \
  "http://localhost:8080/api/v1/kb/_table/kb_integration?filter=name%3D%27dave%27"
```

### 4.5 查看表结构（内省）

```bash
curl -H "X-API-Key: faucet_YOUR_KEY" "http://localhost:8080/api/v1/kb/_schema"
```

返回所有表、列、类型映射、主键、默认值等信息；OpenAPI 规范见 `/openapi.json`，MCP 端点供 AI Agent 使用（与 PostgreSQL 服务用法完全一致）。

---

## 5. 兼容性说明与已知差异

| 项 | 说明 |
|---|---|
| 兼容模式 | 仅支持 PG 模式（`SHOW database_mode` 为 `pg`）。Oracle/MySQL 模式不适用 |
| 方言能力 | `$n` 参数化、`RETURNING`、`ON CONFLICT` upsert、事务均已支持并有集成测试覆盖 |
| 宽类型 | numeric / bool / timestamptz / jsonb / bytea / 数组 往返验证通过 |
| **数组参数** | 通过 REST/驱动传入数组时需使用 PG 数组字面量（如 `{"a","b"}`）；gokb 不接受原生 Go 切片参数（pgx 接受），JSON 数组直插数组列暂有限制 |
| 国密认证 | gokb 内置 SM3/SCRAM 支持，适配信创场景下配置了国密认证的金仓实例；pgx 等第三方驱动无此能力 |
| 为什么不用 pgx 直连 | pg 模式下 pgx 大部分场景可用，但无国密支持、无官方背书、特有类型可能有差异；Faucet 选择官方 gokb 驱动路线 |

---

## 6. 常见问题排查

**连接被拒绝 / Service not found**
- 确认端口是 **54321** 而非 5432
- `db add` 后是否重启过 `faucet serve`（服务列表不热加载）

**报错 `unsupported sslmode "prefer"`**
- 从 PostgreSQL 复制的 DSN 带了 `sslmode=prefer`，gokb 不支持，改为 `disable` / `require` / `verify-ca` / `verify-full`

**认证失败**
- 检查用户名密码；容器镜像的默认账户由 `DB_USER` / `DB_PASSWORD` 环境变量决定
- 远程 TCP 连接走 `scram-sha-256` 认证（`sys_hba.conf` 控制）

**驱动相关报错（`kb:` 前缀）**
- `kb:` 前缀的错误来自 gokb 驱动/金仓服务端；参数化相关的 `there is no parameter $N` 通常意味着 filter 引用了占位符但缺少参数

---

## 7. 运行集成测试（开发者）

针对 KingbaseES 的完整集成测试套件（DDL 默认值、CRUD 管道、upsert/事务回滚、宽类型、存储过程内省）位于 `internal/connector/kingbase/`，需要本地容器：

```bash
# 1. 启动测试容器（同第 1.2 节）
# 2. 运行
FAUCET_INTEGRATION=1 go test -count=1 -v ./internal/connector/kingbase/

# 指定其他实例
KINGBASE_TEST_DSN="kingbase://user:pass@host:54321/db?sslmode=disable" \
  FAUCET_INTEGRATION=1 go test -count=1 ./internal/connector/kingbase/
```

无 `FAUCET_INTEGRATION` 环境变量时测试自动跳过（CI 安全）。
