import { createContext, useContext, useState, useCallback, type ReactNode } from "react";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { AuthService, LoginRequestSchema } from "@buf/wolotec_certship.bufbuild_es/certship/v1/auth_pb";
import { transport } from "@/api/transport";

interface AuthContextType {
  token: string | null;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType>(null!);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState<string | null>(() => localStorage.getItem("token"));

  const login = useCallback(async (username: string, password: string) => {
    const client = createClient(AuthService, transport);
    const res = await client.login(create(LoginRequestSchema, { username, password }));
    localStorage.setItem("token", res.token);
    setToken(res.token);
  }, []);

  const logout = useCallback(() => {
    localStorage.removeItem("token");
    setToken(null);
  }, []);

  return (
    <AuthContext value={{ token, login, logout }}>
      {children}
    </AuthContext>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}
