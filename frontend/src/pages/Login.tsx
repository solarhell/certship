import { useState } from "react";
import { useNavigate } from "react-router";
import { Card, Form, Input, Button, Typography, App } from "antd";
import { SafetyCertificateOutlined, UserOutlined, LockOutlined } from "@ant-design/icons";
import { useAuth } from "@/auth/AuthContext";

export default function Login() {
  const { login } = useAuth();
  const navigate = useNavigate();
  const { message } = App.useApp();
  const [loading, setLoading] = useState(false);

  const onFinish = async (values: { username: string; password: string }) => {
    setLoading(true);
    try {
      await login(values.username, values.password);
      await navigate("/", { replace: true });
    } catch (err) {
      message.error(err instanceof Error ? err.message : "登录失败");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div
      style={{
        display: "flex",
        justifyContent: "center",
        alignItems: "center",
        minHeight: "100vh",
        background: "linear-gradient(135deg, #667eea 0%, #764ba2 100%)",
      }}
    >
      <Card style={{ width: 400, borderRadius: 8 }} styles={{ body: { padding: 32 } }}>
        <div style={{ textAlign: "center", marginBottom: 32 }}>
          <SafetyCertificateOutlined style={{ fontSize: 48, color: "#1677ff" }} />
          <Typography.Title level={3} style={{ marginTop: 12, marginBottom: 0 }}>
            CertShip
          </Typography.Title>
          <Typography.Text type="secondary">阿里云 OSS 证书自动管理</Typography.Text>
        </div>
        <Form onFinish={onFinish} size="large">
          <Form.Item name="username" rules={[{ required: true, message: "请输入用户名" }]}>
            <Input prefix={<UserOutlined />} placeholder="用户名" />
          </Form.Item>
          <Form.Item name="password" rules={[{ required: true, message: "请输入密码" }]}>
            <Input.Password prefix={<LockOutlined />} placeholder="密码" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" block loading={loading}>
              登录
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
