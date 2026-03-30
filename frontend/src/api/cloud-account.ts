import { rpc } from "./client";

const SVC = "CloudAccountService";

export interface CloudAccountItem {
  id: string;
  name: string;
  accessKeyId: string;
  enabled: boolean;
  createdAt: string;
}

export interface ListCloudAccountsResponse {
  accounts: CloudAccountItem[];
}

export interface CreateCloudAccountRequest {
  name: string;
  accessKeyId: string;
  accessKeySecret: string;
  enabled: boolean;
}

export interface UpdateCloudAccountRequest {
  id: string;
  name: string;
  accessKeyId: string;
  accessKeySecret: string;
  enabled: boolean;
}

export function listCloudAccounts() {
  return rpc<object, ListCloudAccountsResponse>(SVC, "ListCloudAccounts", {});
}

export function createCloudAccount(req: CreateCloudAccountRequest) {
  return rpc<CreateCloudAccountRequest, { id: string }>(SVC, "CreateCloudAccount", req);
}

export function updateCloudAccount(req: UpdateCloudAccountRequest) {
  return rpc<UpdateCloudAccountRequest, object>(SVC, "UpdateCloudAccount", req);
}

export function deleteCloudAccount(id: string) {
  return rpc<{ id: string }, object>(SVC, "DeleteCloudAccount", { id });
}
