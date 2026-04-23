import { useEffect, useState, useCallback } from "react";
import { Table, Tag, Button, App, Popconfirm, Card, Input, Select, Space } from "antd";
import { SyncOutlined, ReloadOutlined, SearchOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { CertificateService, ListCertificatesRequestSchema, type CertificateItem } from "@buf/wolotec_certship.bufbuild_es/certship/v1/certificate_pb";
import { RenewTaskService, CreateRenewTaskRequestSchema } from "@buf/wolotec_certship.bufbuild_es/certship/v1/renew_task_pb";
import { transport } from "@/api/transport";
import { useAutoRefresh, REFRESH_OPTIONS } from "@/hooks/useAutoRefresh";

const statusMap = {
  active: { color: "success" as const, text: "正常" },
  pending: { color: "processing" as const, text: "待处理" },
  error: { color: "error" as const, text: "异常" },
};

export default function Certificates() {
  const { message } = App.useApp();
  const [data, setData] = useState<CertificateItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<string | null>(null);

  const fetchData = useCallback(() => {
    setLoading(true);
    const client = createClient(CertificateService, transport);
    client.listCertificates(create(ListCertificatesRequestSchema, { limit: BigInt(100) }))
      .then((res) => setData([...res.certificates]))
      .finally(() => setLoading(false));
  }, []);

  const { interval: refreshInterval, setInterval: setRefreshInterval } = useAutoRefresh(fetchData);
  useEffect(() => { fetchData(); }, [fetchData]);

  const handleRenew = async (domains: string[]) => {
    try {
      const client = createClient(RenewTaskService, transport);
      await client.createRenewTask(create(CreateRenewTaskRequestSchema, { domains }));
      message.success(`已创建续期任务：${domains.join(", ")}`);
      setSelectedRowKeys([]);
    } catch (err) {
      message.error(err instanceof Error ? err.message : "创建任务失败");
    }
  };

  const filtered = data.filter((c) => {
    if (statusFilter && c.status !== statusFilter) return false;
    if (search && !c.domain.includes(search) && !c.bucket.includes(search) && !c.accountName.includes(search)) return false;
    return true;
  });

  const columns: ColumnsType<CertificateItem> = [
    { title: "域名", dataIndex: "domain", width: 280 },
    { title: "Bucket", dataIndex: "bucket", width: 200, ellipsis: true },
    { title: "部署", dataIndex: "deployTarget", width: 70, align: "center", render: (v: string) => <Tag color={v === "cdn" ? "orange" : "blue"}>{v === "cdn" ? "CDN" : "OSS"}</Tag> },
    { title: "状态", dataIndex: "status", width: 70, align: "center", render: (s: string) => { const m = statusMap[s as keyof typeof statusMap]; return m ? <Tag color={m.color}>{m.text}</Tag> : s; } },
    { title: "过期时间", dataIndex: "expiresAt", width: 155, render: (v: string) => v ? dayjs(v).format("YYYY-MM-DD HH:mm") : "-" },
    { title: "剩余", width: 80, align: "center", render: (_: unknown, record: CertificateItem) => {
      if (!record.expiresAt) return "-";
      const days = dayjs(record.expiresAt).diff(dayjs(), "day");
      const color = days <= 0 ? "red" : days <= 7 ? "red" : days <= 30 ? "orange" : "green";
      return <Tag color={color}>{days <= 0 ? "已过期" : `${days}天`}</Tag>;
    } },
    { title: "操作", width: 70, align: "center", render: (_, record) => (
      <Popconfirm title="确认续期？" onConfirm={() => handleRenew([record.domain])}><a><SyncOutlined /> 续期</a></Popconfirm>
    ) },
  ];

  const selectedDomains = data.filter((c) => selectedRowKeys.includes(c.id)).map((c) => c.domain);

  return (
    <Card title="证书列表" extra={
      <Space>
        <Input prefix={<SearchOutlined />} placeholder="搜索" allowClear value={search} onChange={(e) => setSearch(e.target.value)} style={{ width: 160 }} />
        <Select placeholder="状态" allowClear value={statusFilter} onChange={setStatusFilter} style={{ width: 100 }} options={[
          { value: "active", label: "正常" }, { value: "pending", label: "待处理" }, { value: "error", label: "异常" },
        ]} />
        <Select value={refreshInterval} onChange={setRefreshInterval} style={{ width: 140 }} options={REFRESH_OPTIONS} />
        <Button icon={<ReloadOutlined />} onClick={fetchData}>刷新</Button>
        {selectedRowKeys.length > 0 && (
          <Button type="primary" icon={<SyncOutlined />} onClick={() => handleRenew(selectedDomains)}>批量续期</Button>
        )}
      </Space>
    }>
      <Table<CertificateItem> rowKey="id" columns={columns} dataSource={filtered} loading={loading}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 条` }} size="middle"
        rowSelection={{ selectedRowKeys, onChange: setSelectedRowKeys }}
        scroll={{ x: 880 }} />
    </Card>
  );
}
