import { useEffect, useState } from "react";
import { Table, Button, Popconfirm, Switch, App, Card, Modal, Form, Input } from "antd";
import { PlusOutlined, ReloadOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { listCloudAccounts, createCloudAccount, updateCloudAccount, deleteCloudAccount, type CloudAccountItem } from "@/api/cloud-account";

export default function CloudAccounts() {
  const { message } = App.useApp();
  const [data, setData] = useState<CloudAccountItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<CloudAccountItem | null>(null);
  const [confirmLoading, setConfirmLoading] = useState(false);
  const [form] = Form.useForm();

  const fetchData = () => { setLoading(true); listCloudAccounts().then((res) => setData(res.accounts ?? [])).finally(() => setLoading(false)); };
  useEffect(() => { fetchData(); }, []);

  const openModal = (record?: CloudAccountItem) => {
    setEditing(record ?? null);
    form.setFieldsValue(record ? { name: record.name, accessKeyId: record.accessKeyId, accessKeySecret: "", enabled: record.enabled } : { enabled: true });
    setModalOpen(true);
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setConfirmLoading(true);
      if (editing) { await updateCloudAccount({ id: editing.id, ...values }); message.success("更新成功"); }
      else { await createCloudAccount(values); message.success("创建成功"); }
      setModalOpen(false); setEditing(null); form.resetFields(); fetchData();
    } catch { /* validation */ } finally { setConfirmLoading(false); }
  };

  const handleDelete = async (id: string) => { await deleteCloudAccount(id); message.success("删除成功"); fetchData(); };

  const columns: ColumnsType<CloudAccountItem> = [
    { title: "名称", dataIndex: "name", ellipsis: true },
    { title: "Access Key ID", dataIndex: "accessKeyId", ellipsis: true },
    { title: "启用", dataIndex: "enabled", width: 80, render: (v: boolean) => <Switch checked={v} disabled size="small" /> },
    { title: "创建时间", dataIndex: "createdAt", width: 170, render: (v: string) => v ? dayjs(v).format("YYYY-MM-DD HH:mm") : "-" },
    { title: "操作", width: 120, fixed: "right", render: (_, record) => (
      <><a onClick={() => openModal(record)}>编辑</a><Popconfirm title="确认删除？" onConfirm={() => handleDelete(record.id)}><a style={{ color: "#ff4d4f", marginLeft: 12 }}>删除</a></Popconfirm></>
    ) },
  ];

  return (
    <Card title="云账号管理" extra={<><Button icon={<ReloadOutlined />} onClick={fetchData} style={{ marginRight: 8 }}>刷新</Button><Button type="primary" icon={<PlusOutlined />} onClick={() => openModal()}>新增账号</Button></>}>
      <Table<CloudAccountItem> rowKey="id" columns={columns} dataSource={data} loading={loading} pagination={{ pageSize: 20 }} size="middle" />
      <Modal title={editing ? "编辑云账号" : "新增云账号"} open={modalOpen} onOk={handleSubmit} onCancel={() => { setModalOpen(false); setEditing(null); form.resetFields(); }} confirmLoading={confirmLoading} destroyOnClose>
        <Form form={form} layout="vertical" preserve={false}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="accessKeyId" label="Access Key ID" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="accessKeySecret" label="Access Key Secret" rules={editing ? [] : [{ required: true }]}><Input.Password placeholder={editing ? "留空则不修改" : undefined} /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}
