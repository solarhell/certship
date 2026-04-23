import { useEffect, useState } from "react";
import { Table, Button, Popconfirm, Switch, App, Card, Modal, Form, Input } from "antd";
import { PlusOutlined, ReloadOutlined, SyncOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import {
  CloudAccountService, ListCloudAccountsRequestSchema, CreateCloudAccountRequestSchema,
  UpdateCloudAccountRequestSchema, DeleteCloudAccountRequestSchema, RescanCloudAccountRequestSchema,
  type CloudAccountItem,
} from "@buf/wolotec_certship.bufbuild_es/certship/v1/cloud_account_pb";
import { transport } from "@/api/transport";

export default function CloudAccounts() {
  const { message } = App.useApp();
  const [data, setData] = useState<CloudAccountItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<CloudAccountItem | null>(null);
  const [confirmLoading, setConfirmLoading] = useState(false);
  const [rescanningId, setRescanningId] = useState<string | null>(null);
  const [form] = Form.useForm();

  const client = createClient(CloudAccountService, transport);

  const rescan = async (id: string, name: string) => {
    setRescanningId(id);
    try {
      const res = await client.rescanCloudAccount(create(RescanCloudAccountRequestSchema, { id }));
      message.success(`${name}:新增 ${res.added} 个域名，更新 ${res.updated} 个，共 ${res.total} 个`);
    } catch (err) {
      message.error(`扫描失败：${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setRescanningId(null);
    }
  };

  const fetchData = () => {
    setLoading(true);
    client.listCloudAccounts(create(ListCloudAccountsRequestSchema, {}))
      .then((res) => setData([...res.accounts]))
      .finally(() => setLoading(false));
  };
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
      if (editing) {
        await client.updateCloudAccount(create(UpdateCloudAccountRequestSchema, { id: editing.id, ...values }));
        message.success("更新成功");
        setModalOpen(false); setEditing(null); form.resetFields(); fetchData();
      } else {
        const res = await client.createCloudAccount(create(CreateCloudAccountRequestSchema, values));
        message.success("创建成功");
        setModalOpen(false); setEditing(null); form.resetFields(); fetchData();
        if (values.enabled) {
          rescan(res.id, values.name);
        }
      }
    } catch { /* validation */ } finally { setConfirmLoading(false); }
  };

  const handleDelete = async (id: string) => {
    await client.deleteCloudAccount(create(DeleteCloudAccountRequestSchema, { id }));
    message.success("删除成功"); fetchData();
  };

  const columns: ColumnsType<CloudAccountItem> = [
    { title: "名称", dataIndex: "name", ellipsis: true },
    { title: "Access Key ID", dataIndex: "accessKeyId", ellipsis: true },
    { title: "启用", dataIndex: "enabled", width: 80, render: (v: boolean) => <Switch checked={v} disabled size="small" /> },
    { title: "创建时间", dataIndex: "createdAt", width: 170, render: (v: string) => v ? dayjs(v).format("YYYY-MM-DD HH:mm") : "-" },
    { title: "操作", width: 200, fixed: "right", render: (_, record) => (
      <>
        <Button type="link" size="small" icon={<SyncOutlined spin={rescanningId === record.id} />}
          disabled={!record.enabled || rescanningId !== null}
          onClick={() => rescan(record.id, record.name)}>扫描</Button>
        <a onClick={() => openModal(record)}>编辑</a>
        <Popconfirm title="确认删除？" onConfirm={() => handleDelete(record.id)}>
          <a style={{ color: "#ff4d4f", marginLeft: 12 }}>删除</a>
        </Popconfirm>
      </>
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
