# certship

自动为阿里云 OSS / CDN 自定义域名签发并续期 SSL 证书的服务，带 Web 管理界面。

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

## 特性

- **自动发现**：并列扫描 OSS bucket 的自定义域名与 CDN 加速域名，合并成一份云上现状
- **自动签发**：通过 Let's Encrypt + 阿里云 DNS（AliDNS）DNS-01 校验签发证书
- **自动部署**：签发后自动上传到阿里云 OSS 自定义域名 / CDN（域名出现在 CDN 上就部署到 CDN，bucket 视为回源源站）
- **自动续期**：内置调度器定时扫描临期证书并续期
- **云端资源为准**：每次刷新都从阿里云获取当前资源，已删除的 bucket / 域名自动退出列表和续期范围
- **失败退避**：按错误类型区分可重试与需人工介入，避免无意义地反复消耗 Let's Encrypt 配额
- **并发续期**：DNS-01 挑战等待 TXT 传播的时间可以重叠，批量续期不再是串行等待
- **多账号**：支持配置多个阿里云 AccessKey，管理多套独立的 OSS / CDN 资源
- **Web UI**：管理证书、续期任务、云账号、通知渠道与系统设置
- **通知**：续期成功/失败推送到飞书（Lark），按状态变化去噪
- **审计**：登录令牌、操作记录入库

## 数据来源与托管状态

阿里云是当前资源的唯一事实来源。数据库不维护另一套资源清单或下线、确认、归档流程。

| 数据 | 来源与用途 |
| --- | --- |
| 当前 bucket、区域、自定义域名、CDN 绑定、部署账号与目标 | 每次刷新从 OSS / CDN 获取；删除后立即退出当前列表和自动续期范围 |
| 线上已部署证书及有效期 | 从实际部署侧获取，不使用本地签发证书的日期替代 |
| 暂停托管、连续失败次数、重试时间、错误与通知状态 | 本地持久保存，表达 certship 的管理意图和任务状态 |
| certship 签发的证书、私钥、ACME 账号身份 | 本地持久保存；先保存签发材料再部署，部署失败后可复用 |
| 续期任务、执行日志、登录令牌 | 本地审计记录，保留操作证据 |

每次界面刷新和定时扫描都重新查询云端。并发任务可共享最长 30 秒的进程内观测，账号变更会使缓存失效；签发前和写入前另行核验实际绑定和账号是否启用。跨账号的 OSS 与 CDN 绑定分别保留所属账号，CDN 部署优先。

必须完整获取所有分页、bucket CNAME 和线上证书后才返回当前列表。查询失败明确报错，不会用旧 DB 记录补齐，更不会把权限不足或超时当成资源已删除。CDN 查询不完整时，不会将部署目标降级为 OSS；线上证书查询失败时，不会据此重新签发。

数据库中的旧 bucket、region、account_name、origin、deploy_target、presence、last_seen_at 字段，以及旧的下线/归档设置仅为升级兼容保留，不再决定当前资源、续期、部署或自动删除。历史本地托管设置和签发材料保留供域名重新出现时复用，但不作为云资源展示。

## 失败是怎么处理的

签发和部署是解耦的两步：部署前先确认目标侧还认这个域名（OSS 查 cname 是否还绑着、CDN 查是否 online），
预检不过就绝不去 ACME 签证书；证书够新时重试只重跑部署，不重新签发。所以"签发成功、部署失败"的域名
反复重试也不会消耗 Let's Encrypt 配额。

错误按类型分流：域名归属校验失败、AK 失效这类**永久错误**直接写入阻塞原因并停止自动重试；
网络抖动、服务端 5xx 这类**可重试错误**按指数退避，试满次数后转人工；**被限速**时等待超过一个扫描周期，
避免每日重试持续撑满 CA 的滚动窗口。阻塞后在界面上手动点一次"续期"即可解除。

告警只在状态变化时推送（首次失败、恢复正常），持续异常每周提醒一次，不会每轮刷屏。

## Roadmap

- [ ] 支持 Cloudflare 托管域名：为 Cloudflare 免费版 Universal SSL 未覆盖的多级子域（如 `*.foo.example.com`）自动签发证书
- [ ] 更多部署目标
- [ ] docker-compose / systemd 部署模板

## 快速开始

### 前置

- 阿里云 AccessKey，需有以下权限：
  - **AliDNS**：读写 DNS 记录（用于 Let's Encrypt DNS-01 校验）
  - **OSS**：管理 Bucket 自定义域名及证书
  - **CDN**：管理加速域名及证书

### 后端

```bash
cd backend

# 初始化数据库（首次）
createdb certship

# 复制配置
cp configs/config.example.toml configs/config.toml
# 编辑 configs/config.toml 填入数据库连接信息

# 安装 just（brew install just）并安装工具链（首次）
cd backend && just init

# 本地构建并运行
just build-local
./certship --config configs/config.toml --addr 127.0.0.1:8080
```

首次启动时，certship 会自动创建管理员账号 `admin` 并将**随机生成的初始密码打印到日志**，登录后请立即修改：

```
WARN  已创建默认管理员账号，请立即登录并修改密码。此密码仅显示一次。
      username=admin password=xxxxxxxxxxxxxxxxxxxxxxxx
```

### 前端

```bash
cd frontend

# 安装依赖
vp i

# 开发模式（默认请求同源 /api，通过 vite 代理转发到后端）
vp dev

# 生产构建
# 若前后端非同源，先创建 .env.production.local 写 VITE_API_BASE
vp build
```

### 配置

`backend/configs/config.toml` 当前只需填数据库连接信息，其它配置均在 Web UI 中管理并存入数据库：

| 配置项 | 默认值 | 说明 |
| --- | --- | --- |
| 扫描间隔 | `24h` | 一个完整周期：扫描云上现状 → 加载托管设置 → 补部署 → 续期 |
| 提前续期天数 | `30` | 证书剩余有效期低于它就续期；也是"证书够不够新、能否直接复用"的判定线 |
| DNS 解析器 | `223.5.5.5:53,119.29.29.29:53` | zone 探测与 DNS-01 校验使用的递归 DNS，逗号分隔 |

首次启动会自动创建这行配置并落默认值，不需要手工往库里插数据。

DNS 解析器之所以要显式配置而不是用系统的：服务器上的 `/etc/resolv.conf` 可能指向内网 DNS 或被劫持，
拿到的 SOA/NS 与公网不一致，会让 zone 判定张冠李戴——而这个判定直接决定用哪个云账号做 DNS-01 挑战。
zone 探测、NS 归属校验、TXT 传播检查三处用的是同一组解析器，结论不会互相打架。

ACME 的注册邮箱与目录地址固定在代码里（`internal/acme/manager.go`），不对外暴露：
目录地址是全局的，改成 staging 会让**所有**域名签出不被信任的证书并自动部署到线上，
而且 ACME 账号绑定 CA，切换后存量注册信息立刻失效。需要演练就另起一个实例连独立的库。

```toml
[database]
host = "127.0.0.1"
port = 5432
username = "certship"
password = "your-password"
db = "certship"
```

完整字段见 [`backend/configs/config.example.toml`](backend/configs/config.example.toml)。

## 项目结构

```
backend/
  cmd/certship/        # 入口
  internal/
    apiserver/         # HTTP/Connect-RPC server
    daemon/            # 续期调度 daemon
    acme/              # Let's Encrypt 集成
    alidns/            # 阿里云 DNS 操作
    oss/, cdn/         # 阿里云 OSS / CDN 部署
    notify/            # 飞书通知
    config/            # 配置加载
  pkg/
    api/, ent/         # 自动生成（buf、ent）
    entschema/         # ent schema 定义
    logic/             # RPC handler 实现
    module/            # 通用模块（password, jwt 等）
  proto/               # protobuf 定义（同步到 buf.build/certship/api）
frontend/
  src/                 # React 应用
```

## 从 Lego v4 升级

服务兼容读取数据库中已有的 Lego v4 账号注册信息和私钥，无需重新注册 ACME 账号。
新注册账号使用 Lego v5 格式；账号记录损坏或丢失原私钥时会报错，避免意外创建新身份。

相同 DNS 解析器配置下，证书签发继续并发执行。修改解析器配置后，使用新配置的签发会等待旧配置的在途签发结束，避免 Lego v5 的全局 DNS client 在签发过程中被切换。

## 开发

RPC 后端使用 [Connect Go v2](https://connectrpc.com/docs/go/migrating-to-v2/)。Proto 接口依赖 BSR 生成，`backend/buf.gen.yaml` 中的远程插件不固定版本。

```bash
# 后端 lint（go vet + golangci-lint + staticcheck）
cd backend && just lint

# ent schema 修改后重新生成
just ent
```

## License

[MIT](./LICENSE)

### 数据一致性回归

`go test -race ./...` 覆盖资源清单为空、分页/查询不完整、CDN 降级保护、跨账号部署、云端证书日期优先和并发任务缓存等场景。

本地 PostgreSQL 上可额外验证旧表迁移、托管意图保留、部署失败后复用签发材料及任务并发领取。测试自动创建并删除独立数据库，不调用阿里云或 ACME：

```bash
CERTSHIP_TEST_POSTGRES_DSN='host=127.0.0.1 port=5432 user=postgres password=test dbname=postgres sslmode=disable' go test -race ./internal/daemon -run TestDatabaseCompatibilityAndDeploymentRetry
```
