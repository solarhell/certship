import { useEffect, useState } from "react";
import { Card, Form, Input, InputNumber, Button, Spin, App } from "antd";
import { getAppSettings, updateAppSettings, type AppSettings } from "@/api/settings";

export default function Settings() {
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => { getAppSettings().then((data) => form.setFieldsValue(data)).finally(() => setLoading(false)); }, [form]);

  const onFinish = async (values: AppSettings) => {
    setSaving(true);
    try { await updateAppSettings(values); message.success("保存成功"); }
    catch (err) { message.error(err instanceof Error ? err.message : "保存失败"); }
    finally { setSaving(false); }
  };

  return (
    <Card title="系统设置">
      <Spin spinning={loading}>
        <Form form={form} layout="vertical" onFinish={onFinish} style={{ maxWidth: 600 }}>
          <Form.Item name="scanInterval" label="扫描间隔" rules={[{ required: true, message: "请输入扫描间隔" }]} tooltip="Go duration 格式，例如 24h、12h30m">
            <Input placeholder="24h" />
          </Form.Item>
          <Form.Item name="renewBeforeDays" label="提前续期天数" rules={[{ required: true, message: "请输入天数" }]} tooltip="证书到期前多少天自动续期">
            <InputNumber min={1} max={90} style={{ width: "100%" }} />
          </Form.Item>
          <Form.Item><Button type="primary" htmlType="submit" loading={saving}>保存设置</Button></Form.Item>
        </Form>
      </Spin>
    </Card>
  );
}
