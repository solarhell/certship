import { rpc } from "./client";

const SVC = "RenewTaskService";

export interface RenewTaskItem {
  id: string;
  domains: string[];
  status: "pending" | "running" | "success" | "failed";
  trigger: "auto" | "manual";
  errorMessage: string;
  startedAt: string;
  finishedAt: string;
  createdAt: string;
}

export interface ListRenewTasksResponse {
  tasks: RenewTaskItem[];
}

export function listRenewTasks() {
  return rpc<object, ListRenewTasksResponse>(SVC, "ListRenewTasks", {});
}

export function createRenewTask(domains: string[]) {
  return rpc<{ domains: string[] }, { id: string }>(SVC, "CreateRenewTask", { domains });
}
