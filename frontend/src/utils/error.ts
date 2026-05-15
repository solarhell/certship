import { ConnectError, Code } from "@connectrpc/connect";

const codeToZh: Record<number, string> = {
  [Code.Canceled]: "已取消",
  [Code.Unknown]: "未知错误",
  [Code.InvalidArgument]: "参数错误",
  [Code.DeadlineExceeded]: "请求超时",
  [Code.NotFound]: "资源不存在",
  [Code.AlreadyExists]: "已存在",
  [Code.PermissionDenied]: "无权限",
  [Code.ResourceExhausted]: "配额超限",
  [Code.FailedPrecondition]: "状态不满足",
  [Code.Aborted]: "已中止",
  [Code.OutOfRange]: "超出范围",
  [Code.Unimplemented]: "未实现",
  [Code.Internal]: "服务内部错误",
  [Code.Unavailable]: "服务不可用",
  [Code.DataLoss]: "数据损坏",
  [Code.Unauthenticated]: "未登录",
};

// 把任意错误格式化为便于前端展示的中文消息。
export function formatError(err: unknown): string {
  if (err instanceof ConnectError) {
    const prefix = codeToZh[err.code] ?? `code=${err.code}`;
    return `${prefix}:${err.rawMessage}`;
  }
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return JSON.stringify(err);
}
