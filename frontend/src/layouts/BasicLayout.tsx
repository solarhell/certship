import { useState } from "react";
import { Outlet, useNavigate, useLocation } from "react-router";
import { Layout, Menu, Dropdown, Modal, Form, Input, App, theme } from "antd";
import {
  DashboardOutlined, SafetyCertificateOutlined, SyncOutlined, CloudServerOutlined,
  BellOutlined, SettingOutlined, LogoutOutlined, KeyOutlined, UserOutlined,
  MenuFoldOutlined, MenuUnfoldOutlined,
} from "@ant-design/icons";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { AuthService, ChangePasswordRequestSchema } from "@buf/certship_api.bufbuild_es/certship/v1/auth_pb";
import { transport } from "@/api/transport";
import { useAuth } from "@/auth/AuthContext";

const { Header, Sider, Content } = Layout;

const menuItems = [
  { key: "/", icon: <DashboardOutlined />, label: "仪表盘" },
  { key: "/certificates", icon: <SafetyCertificateOutlined />, label: "证书管理" },
  { key: "/renew-tasks", icon: <SyncOutlined />, label: "续期任务" },
  { key: "/cloud-accounts", icon: <CloudServerOutlined />, label: "云账号" },
  { key: "/notifications", icon: <BellOutlined />, label: "通知渠道" },
  { key: "/settings", icon: <SettingOutlined />, label: "系统设置" },
];

export default function BasicLayout() {
  const navigate = useNavigate();
  const location = useLocation();
  const { logout } = useAuth();
  const { message } = App.useApp();
  const { token: themeToken } = theme.useToken();
  const [collapsed, setCollapsed] = useState(false);
  const [pwdOpen, setPwdOpen] = useState(false);
  const [pwdLoading, setPwdLoading] = useState(false);
  const [form] = Form.useForm();

  const handleChangePwd = async () => {
    try {
      const values = await form.validateFields();
      setPwdLoading(true);
      const client = createClient(AuthService, transport);
      await client.changePassword(create(ChangePasswordRequestSchema, {
        oldPassword: values.oldPassword as string,
        newPassword: values.newPassword as string,
      }));
      message.success("密码修改成功");
      setPwdOpen(false);
      form.resetFields();
    } catch {
      // validation or api error
    } finally {
      setPwdLoading(false);
    }
  };

  return (
    <>
      <Layout style={{ minHeight: "100vh" }}>
        <Sider collapsible collapsed={collapsed} onCollapse={setCollapsed} theme="light" style={{ borderRight: `1px solid ${themeToken.colorBorderSecondary}` }}>
          <div style={{ height: 48, display: "flex", alignItems: "center", justifyContent: "center", fontWeight: 600, fontSize: collapsed ? 16 : 18 }}>
            {collapsed ? "CS" : "CertShip"}
          </div>
          <Menu mode="inline" selectedKeys={[location.pathname]} items={menuItems} onClick={({ key }) => navigate(key)} />
        </Sider>
        <Layout>
          <Header style={{ background: themeToken.colorBgContainer, padding: "0 24px", display: "flex", alignItems: "center", justifyContent: "space-between", borderBottom: `1px solid ${themeToken.colorBorderSecondary}` }}>
            <span style={{ cursor: "pointer", fontSize: 16 }} onClick={() => setCollapsed(!collapsed)}>
              {collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
            </span>
            <Dropdown menu={{ items: [
              { key: "password", icon: <KeyOutlined />, label: "修改密码", onClick: () => setPwdOpen(true) },
              { type: "divider" as const },
              { key: "logout", icon: <LogoutOutlined />, label: "退出登录", onClick: () => { logout(); navigate("/login"); } },
            ] }}>
              <span style={{ cursor: "pointer" }}><UserOutlined /> admin</span>
            </Dropdown>
          </Header>
          <Content style={{ margin: 24 }}><Outlet /></Content>
        </Layout>
      </Layout>
      <Modal title="修改密码" open={pwdOpen} onOk={handleChangePwd} onCancel={() => { setPwdOpen(false); form.resetFields(); }} confirmLoading={pwdLoading}>
        <Form form={form} layout="vertical">
          <Form.Item name="oldPassword" label="当前密码" rules={[{ required: true, message: "请输入当前密码" }]}><Input.Password /></Form.Item>
          <Form.Item name="newPassword" label="新密码" rules={[{ required: true, message: "请输入新密码" }, { min: 6, message: "密码至少6位" }]}><Input.Password /></Form.Item>
          <Form.Item name="confirmPassword" label="确认密码" dependencies={["newPassword"]} rules={[
            { required: true, message: "请确认新密码" },
            ({ getFieldValue }) => ({ validator(_, value) { if (!value || getFieldValue("newPassword") === value) return Promise.resolve(); return Promise.reject(new Error("两次输入的密码不一致")); } }),
          ]}><Input.Password /></Form.Item>
        </Form>
      </Modal>
    </>
  );
}
