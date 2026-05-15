# certship

自动为阿里云 OSS / CDN 自定义域名签发并续期 SSL 证书的服务，带 Web 管理界面。

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

## 特性

- **自动签发**：通过 Let's Encrypt + 阿里云 DNS（AliDNS）DNS-01 校验签发证书
- **自动部署**：签发后自动上传到阿里云 OSS 自定义域名 / CDN（根据域名实际归属自动选择目标）
- **自动续期**：内置调度器定时扫描临期证书并续期
- **多账号**：支持配置多个阿里云 AccessKey，管理多套独立的 OSS / CDN 资源
- **Web UI**：管理证书、续期任务、云账号、通知渠道与系统设置
- **通知**：续期成功/失败可推送到飞书（Lark）
- **审计**：登录令牌、操作记录入库

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

`backend/configs/config.toml` 当前只需填数据库连接信息，其它配置（ACME 邮箱、调度间隔、续期阈值、阿里云账号、通知渠道）均在 Web UI 中管理并存入数据库。

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
