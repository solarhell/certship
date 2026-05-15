import { useEffect, useState } from "react";
import { Table, Button, Popconfirm, Switch, App, Card, Modal, Form, Input } from "antd";
import { PlusOutlined, ReloadOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import {
  NotificationChannelService, ListNotificationChannelsRequestSchema, CreateNotificationChannelRequestSchema,
  UpdateNotificationChannelRequestSchema, DeleteNotificationChannelRequestSchema, type NotificationChannelItem,
} from "@buf/certship_api.bufbuild_es/certship/v1/notification_channel_pb";
import { transport } from "@/api/transport";
import { formatError } from "@/utils/error";

export default function Notifications() {
  const { message } = App.useApp();
  const [data, setData] = useState<NotificationChannelItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<NotificationChannelItem | null>(null);
  const [confirmLoading, setConfirmLoading] = useState(false);
  const [form] = Form.useForm();

  const client = createClient(NotificationChannelService, transport);

  const fetchData = () => {
    setLoading(true);
    client.listNotificationChannels(create(ListNotificationChannelsRequestSchema, {}))
      .then((res) => setData([...res.channels]))
      .catch((err) => message.error(`加载通知渠道失败:${formatError(err)}`))
      .finally(() => setLoading(false));
  };
  useEffect(() => { fetchData(); }, []);

  const openModal = (record?: NotificationChannelItem) => {
    setEditing(record ?? null);
    form.setFieldsValue(record ? { name: record.name, webhookUrl: record.webhookUrl, enabled: record.enabled } : { enabled: true });
    setModalOpen(true);
  };

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setConfirmLoading(true);
      if (editing) {
        await client.updateNotificationChannel(create(UpdateNotificationChannelRequestSchema, { id: editing.id, ...values }));
        message.success("更新成功");
      } else {
        await client.createNotificationChannel(create(CreateNotificationChannelRequestSchema, { ...values, type: "feishu" }));
        message.success("创建成功");
      }
      setModalOpen(false); setEditing(null); form.resetFields(); fetchData();
    } catch { /* validation */ } finally { setConfirmLoading(false); }
  };

  const handleDelete = async (id: string) => {
    await client.deleteNotificationChannel(create(DeleteNotificationChannelRequestSchema, { id }));
    message.success("删除成功"); fetchData();
  };

  const columns: ColumnsType<NotificationChannelItem> = [
    { title: "名称", dataIndex: "name", ellipsis: true },
    { title: "类型", dataIndex: "type", width: 100, render: (v: string) => v === "feishu" ? "飞书" : v },
    { title: "Webhook URL", dataIndex: "webhookUrl", ellipsis: true },
    { title: "启用", dataIndex: "enabled", width: 80, render: (v: boolean) => <Switch checked={v} disabled size="small" /> },
    { title: "创建时间", dataIndex: "createdAt", width: 170, render: (v: string) => v ? dayjs(v).format("YYYY-MM-DD HH:mm") : "-" },
    { title: "操作", width: 120, fixed: "right", render: (_, record) => (
      <><a onClick={() => openModal(record)}>编辑</a><Popconfirm title="确认删除？" onConfirm={() => handleDelete(record.id)}><a style={{ color: "#ff4d4f", marginLeft: 12 }}>删除</a></Popconfirm></>
    ) },
  ];

  return (
    <Card title="通知渠道管理" extra={<><Button icon={<ReloadOutlined />} onClick={fetchData} style={{ marginRight: 8 }}>刷新</Button><Button type="primary" icon={<PlusOutlined />} onClick={() => openModal()}>新增渠道</Button></>}>
      <Table<NotificationChannelItem> rowKey="id" columns={columns} dataSource={data} loading={loading} pagination={{ pageSize: 20 }} size="middle" />
      <Modal title={editing ? "编辑通知渠道" : "新增通知渠道"} open={modalOpen} onOk={handleSubmit} onCancel={() => { setModalOpen(false); setEditing(null); form.resetFields(); }} confirmLoading={confirmLoading} destroyOnClose>
        <Form form={form} layout="vertical" preserve={false}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="webhookUrl" label="Webhook URL" rules={[{ required: true, type: "url", message: "请输入有效的 URL" }]}><Input /></Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}
