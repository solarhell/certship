import { useEffect } from "react";
import { BrowserRouter, Routes, Route, Navigate } from "react-router";
import { App as AntApp, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import { AuthProvider, useAuth } from "@/auth/AuthContext";
import { formatError } from "@/utils/error";
import BasicLayout from "@/layouts/BasicLayout";
import Login from "@/pages/Login";
import Dashboard from "@/pages/Dashboard";
import Certificates from "@/pages/Certificates";
import RenewTasks from "@/pages/RenewTasks";
import CloudAccounts from "@/pages/CloudAccounts";
import Notifications from "@/pages/Notifications";
import Settings from "@/pages/Settings";

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { token } = useAuth();
  if (!token) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

// 全局未捕获 Promise 错误兜底,避免接口失败(如 405)时前端静默吞掉
function GlobalErrorCatcher() {
  const { message } = AntApp.useApp();
  useEffect(() => {
    const onRejection = (ev: PromiseRejectionEvent) => {
      const err = ev.reason;
      message.error(formatError(err));
      // eslint-disable-next-line no-console
      console.error("[unhandledrejection]", err);
      ev.preventDefault();
    };
    window.addEventListener("unhandledrejection", onRejection);
    return () => window.removeEventListener("unhandledrejection", onRejection);
  }, [message]);
  return null;
}

function AppRoutes() {
  const { token } = useAuth();
  return (
    <Routes>
      <Route path="/login" element={token ? <Navigate to="/" replace /> : <Login />} />
      <Route
        element={
          <RequireAuth>
            <BasicLayout />
          </RequireAuth>
        }
      >
        <Route path="/" element={<Dashboard />} />
        <Route path="/certificates" element={<Certificates />} />
        <Route path="/renew-tasks" element={<RenewTasks />} />
        <Route path="/cloud-accounts" element={<CloudAccounts />} />
        <Route path="/notifications" element={<Notifications />} />
        <Route path="/settings" element={<Settings />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

export default function App() {
  return (
    <ConfigProvider locale={zhCN}>
      <AntApp>
        <GlobalErrorCatcher />
        <AuthProvider>
          <BrowserRouter>
            <AppRoutes />
          </BrowserRouter>
        </AuthProvider>
      </AntApp>
    </ConfigProvider>
  );
}
