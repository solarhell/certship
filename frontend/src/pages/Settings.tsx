import { useEffect, useState } from "react";
import { Card, Form, Input, InputNumber, Button, Spin, App } from "antd";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { AppSettingsService, GetAppSettingsRequestSchema, UpdateAppSettingsRequestSchema } from "@buf/certship_api.bufbuild_es/certship/v1/app_settings_pb";
import { transport } from "@/api/transport";
import { formatError } from "@/utils/error";

export default function Settings() {
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const client = createClient(AppSettingsService, transport);

  useEffect(() => {
    client.getAppSettings(create(GetAppSettingsRequestSchema, {}))
      .then((data) => form.setFieldsValue({ scanInterval: data.scanInterval, renewBeforeDays: data.renewBeforeDays }))
      .catch((err) => message.error(`加载设置失败:${formatError(err)}`))
      .finally(() => setLoading(false));
  }, [form, client, message]);

  const onFinish = async (values: { scanInterval: string; renewBeforeDays: number }) => {
    setSaving(true);
    try {
      await client.updateAppSettings(create(UpdateAppSettingsRequestSchema, values));
      message.success("保存成功");
    } catch (err) { message.error(`保存失败:${formatError(err)}`); }
    finally { setSaving(false); }
  };

  return (
    <Card title="系统设置">
      <Spin spinning={loading}>
        <Form form={form} layout="vertical" onFinish={onFinish} style={{ maxWidth: 600 }}>
          <Form.Item name="scanInterval" label="扫描间隔" rules={[{ required: true, message: "请输入扫描间隔" }]} tooltip="Go duration 格式，例如 24h、12h30m">
            <Input placeholder="24h" />
          </Form.Item>
          <Form.Item name="renewBeforeDays" label="提前续期天数" rules={[{ required: true, message: "请输入天数" }]} tooltip="证书到期前多少天自动续期(1-60)">
            <InputNumber min={1} max={60} style={{ width: "100%" }} />
          </Form.Item>
          <Form.Item><Button type="primary" htmlType="submit" loading={saving}>保存设置</Button></Form.Item>
        </Form>
      </Spin>
    </Card>
  );
}
