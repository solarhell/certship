# certship

自动为阿里云 OSS / CDN 自定义域名签发并续期 SSL 证书的服务，带 Web 管理界面。

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

## 特性

- **自动发现**：并列扫描 OSS bucket 的自定义域名与 CDN 加速域名，合并成一份云上现状
- **自动签发**：通过 Let's Encrypt + 阿里云 DNS（AliDNS）DNS-01 校验签发证书
- **自动部署**：签发后自动上传到阿里云 OSS 自定义域名 / CDN（域名出现在 CDN 上就部署到 CDN，bucket 视为回源源站）
- **自动续期**：内置调度器定时扫描临期证书并续期
- **下线感知**：域名从云上摘掉后自动标记并停止续期，重新出现会自动恢复托管
- **失败退避**：按错误类型区分可重试与需人工介入，避免无意义地反复消耗 Let's Encrypt 配额
- **并发续期**：DNS-01 挑战等待 TXT 传播的时间可以重叠，批量续期不再是串行等待
- **多账号**：支持配置多个阿里云 AccessKey，管理多套独立的 OSS / CDN 资源
- **Web UI**：管理证书、续期任务、云账号、通知渠道与系统设置
- **通知**：续期成功/失败与域名上下线推送到飞书（Lark），按状态变化去噪
- **审计**：登录令牌、操作记录入库

## 域名生命周期

数据库里的域名记录是**云上现状的投影**：域名存不存在由阿里云决定，certship 每轮扫描做双向对账。

```
                  扫描到                    连续未扫到              再等一段时间
   (云上新域名) ──────────> present ──────────────────> missing ──────────────> archived
                             ^                             │                       │
                             └─────────────────────────────┴───────────────────────┘
                                              域名重新出现，自动恢复托管
```

- `present` 正常托管，参与签发与续期
- `missing` 连续 `missing_grace`（默认 72h）没在云上扫到，暂停续期但保留记录
- `archived` 再过 `archive_after`（默认 168h）仍未出现，归档并停止托管；此时才允许手动删除记录

两个宽限期的意义是抗抖动：运维迁移 bucket 常见"先解绑再绑"，扫描 API 也会限流失败。
对账严格限定在**本轮实际扫通的账号与 bucket** 范围内，扫不到的一律放过，不会因为一次 API 抖动把活着的域名判死。

界面上另有**暂停托管**开关，表达的是"这个域名的证书由别处管理"，与域名在云上是否存在无关。
删除只对 `archived` 开放——云上还在的域名删掉也会被下一轮扫描原样写回来。
归档记录会保留到"归档记录保留期"之后才物理删除，且只删证书也已过期的；证书还没过期就先留着，
万一域名是被误摘的，记录里的证书还能直接复用重绑。

## 失败是怎么处理的

签发和部署是解耦的两步：部署前先确认目标侧还认这个域名（OSS 查 cname 是否还绑着、CDN 查是否 online），
预检不过就绝不去 ACME 签证书；证书够新时重试只重跑部署，不重新签发。所以"签发成功、部署失败"的域名
反复重试也不会消耗 Let's Encrypt 配额。

错误按类型分流：域名归属校验失败、AK 失效这类**永久错误**直接写入阻塞原因并停止自动重试；
网络抖动、服务端 5xx 这类**可重试错误**按指数退避，试满次数后转人工；**被限速**时等待超过一个扫描周期，
避免每日重试持续撑满 CA 的滚动窗口。阻塞后在界面上手动点一次"续期"即可解除。

告警只在状态变化时推送（首次失败、恢复正常、上下线），持续异常每周提醒一次，不会每轮刷屏。

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

# 安装工具链（首次）
make init

# 本地构建并运行
make build-local
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
| 扫描间隔 | `24h` | 一个完整周期：扫描云上现状 → 对账 → 补部署 → 续期 → 清理 |
| 提前续期天数 | `30` | 证书剩余有效期低于它就续期；也是"证书够不够新、能否直接复用"的判定线 |
| 下线判定宽限期 | `72h` | 连续多久没在云上扫到才判定为 missing |
| 归档等待期 | `168h` | 判定 missing 后再过多久归档并停止托管 |
| 归档记录保留期 | `2160h` | 归档且证书已过期的记录多久后物理删除，`0s` 表示永久保留 |
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

## 开发

```bash
# 后端 lint（go vet + golangci-lint + staticcheck）
cd backend && make lint

# proto 修改后重新生成（需配置 buf.build 推送权限）
make proto

# ent schema 修改后重新生成
make ent
```

## License

[MIT](./LICENSE)
