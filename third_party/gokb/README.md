# gokb — KingbaseES Go 驱动（本地 vendored 副本）

## 来源

- 上游：人大金仓官网下载中心 https://www.kingbase.com.cn/download.html#drive
  「接口驱动」栏的 `KingbaseES_V009R001C010B0004_GOLANG.zip`
  （KingbaseES V009R001 Go 语言驱动）
- 兼容性：V9 驱动向后兼容 V8R6 服务端（已通过 faucet 全量单元测试与
  Kingbase 集成测试套件对 V008R006C009B0014 实例实测验证）
- module 路径：`kingbase.com/gokb`（zip 内 go.mod 声明）
- 许可：MIT（lib/pq 派生），见本目录 [LICENSE](LICENSE)（zip 内未附带，
  由本副本补齐——如对外分发请与官方发行包核对最新许可条款）

## 引入方式

`go.mod` 中通过本地 replace 引用，不依赖任何 module proxy：

```
require kingbase.com/gokb v0.0.0-00010101000000-000000000000

replace kingbase.com/gokb => ./third_party/gokb
```

依赖：`github.com/golang-sql/civil`、`github.com/shopspring/decimal`
（zip 自带的 go.mod 漏掉了源码实际 import 的这两个包，已在此副本补全）。

## 用途

为 `internal/connector/kingbase` 提供 `database/sql` 驱动，注册名为
`kingbase`，默认端口 54321。DSN 支持
`kingbase://user:pass@host:54321/db?sslmode=disable` URL 风格与 lib/pq
键值风格两种。

## 升级记录

- 2026-09：V8R6（v8r6_golang.zip）→ V9R1
  （KingbaseES_V009R001C010B0004_GOLANG.zip），差异集中在 conn.go /
  kb_types.go 等 4 文件的类型增强（auto_increment 域类型、多兼容模式
  lastInsertID 处理），核心协议/认证/SSL 层无变化

## 升级方法

从官网下载新版 zip，解压后用其中的 `kingbase.com/gokb` 目录替换本目录
下的 .go 源文件，然后**重做以下手工维护项**（zip 内不含）：

1. go.mod 的 require 块（civil + decimal，见上）——注意保持 `go` 指令
   不高于主模块的版本，否则会抬升整个项目的工具链要求
2. 本 README.md 与 LICENSE
3. 在主项目根目录跑 `go build ./...` 验证
