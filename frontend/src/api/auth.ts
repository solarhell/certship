import { rpc } from "./client";

const SVC = "AuthService";

export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  token: string;
}

export interface ChangePasswordRequest {
  oldPassword: string;
  newPassword: string;
}

export function login(req: LoginRequest) {
  return rpc<LoginRequest, LoginResponse>(SVC, "Login", req);
}

export function changePassword(req: ChangePasswordRequest) {
  return rpc<ChangePasswordRequest, object>(SVC, "ChangePassword", req);
}
