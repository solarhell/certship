# CertShip 前端

React + TypeScript 管理界面，使用 Vite+ 统一管理开发、构建、格式化与 lint。

## 开发与验证

```bash
vp install --frozen-lockfile
vp dev

# 格式、lint 和类型检查
vp check

# 执行 TypeScript 项目构建检查，再生成生产资源
vp run build
```

开发服务器监听 `3000`，将 `/certship.v1` 请求代理到 `http://127.0.0.1:8080`。
生产环境若前后端不同源，在 `.env.production.local` 配置 `VITE_API_BASE`。

## 工具链约定

- 配置参考 [create-vite-plus](https://github.com/voidzero-dev/vite-plus/tree/main/packages/create-vite-plus) 的 Application 生成流程：先使用 Vite React + TypeScript 模板，再应用 Vite+ 工具链配置；保留项目的路径别名、API 代理和严格类型检查。
- `tsconfig.app.json` 使用 bundler 模式；`tsconfig.node.json` 使用 NodeNext 模式检查 Vite 配置。
- lint 使用 Vite+ 内置 Oxlint，启用 React Hooks、React Refresh 与类型感知检查，无需另装 ESLint 或 Oxlint。
- `vite` 指向 `@voidzero-dev/vite-plus-core`，版本与 `vite-plus` 在 `pnpm-workspace.yaml` 的 catalog 中同步维护。
- 使用 `vp` 管理依赖；`vp run build` 执行 `package.json` 中的构建脚本，`vp build` 只运行打包步骤。
- React Compiler 未启用。目前没有前端测试文件。
