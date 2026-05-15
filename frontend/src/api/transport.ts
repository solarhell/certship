import { createConnectTransport } from "@connectrpc/connect-web";
import type { Interceptor } from "@connectrpc/connect";

const API_BASE = import.meta.env.VITE_API_BASE ?? "";

const authInterceptor: Interceptor = (next) => async (req) => {
  const token = localStorage.getItem("token");
  if (token) {
    req.header.set("Authorization", `Bearer ${token}`);
  }
  try {
    return await next(req);
  } catch (err) {
    if (err instanceof Error && "code" in err && (err as { code: number }).code === 16) {
      // Unauthenticated
      localStorage.removeItem("token");
      window.location.href = "/login";
    }
    throw err;
  }
};

export const transport = createConnectTransport({
  baseUrl: API_BASE,
  interceptors: [authInterceptor],
});
