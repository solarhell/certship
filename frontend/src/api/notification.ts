import { rpc } from "./client";

const SVC = "NotificationChannelService";

export interface NotificationChannelItem {
  id: string;
  name: string;
  type: string;
  webhookUrl: string;
  enabled: boolean;
  createdAt: string;
}

export interface ListNotificationChannelsResponse {
  channels: NotificationChannelItem[];
}

export interface CreateNotificationChannelRequest {
  name: string;
  type: string;
  webhookUrl: string;
  enabled: boolean;
}

export interface UpdateNotificationChannelRequest {
  id: string;
  name: string;
  webhookUrl: string;
  enabled: boolean;
}

export function listNotificationChannels() {
  return rpc<object, ListNotificationChannelsResponse>(SVC, "ListNotificationChannels", {});
}

export function createNotificationChannel(req: CreateNotificationChannelRequest) {
  return rpc<CreateNotificationChannelRequest, { id: string }>(SVC, "CreateNotificationChannel", req);
}

export function updateNotificationChannel(req: UpdateNotificationChannelRequest) {
  return rpc<UpdateNotificationChannelRequest, object>(SVC, "UpdateNotificationChannel", req);
}

export function deleteNotificationChannel(id: string) {
  return rpc<{ id: string }, object>(SVC, "DeleteNotificationChannel", { id });
}
