import { createContext, useContext, useState, useCallback, type ReactNode } from "react";
import { login as apiLogin, type LoginRequest } from "@/api/auth";

interface AuthContextType {
  token: string | null;
  login: (req: LoginRequest) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType>(null!);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [token, setToken] = useState<string | null>(() => localStorage.getItem("token"));

  const login = useCallback(async (req: LoginRequest) => {
    const res = await apiLogin(req);
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
