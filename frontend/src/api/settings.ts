import { rpc } from "./client";

const SVC = "AppSettingsService";

export interface AppSettings {
  scanInterval: string;
  renewBeforeDays: number;
}

export function getAppSettings() {
  return rpc<object, AppSettings>(SVC, "GetAppSettings", {});
}

export function updateAppSettings(req: AppSettings) {
  return rpc<AppSettings, object>(SVC, "UpdateAppSettings", req);
}
