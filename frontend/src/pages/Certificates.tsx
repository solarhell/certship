import { useEffect, useState } from "react";
import { Table, Tag, Button, App, Popconfirm, Card, Input, Select, Space } from "antd";
import { SyncOutlined, ReloadOutlined, SearchOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { listCertificates, type CertificateItem } from "@/api/certificate";
import { createRenewTask } from "@/api/renew-task";

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

  const fetchData = () => {
    setLoading(true);
    listCertificates().then((res) => setData(res.certificates ?? [])).finally(() => setLoading(false));
  };

  useEffect(() => { fetchData(); }, []);

  const handleRenew = async (domains: string[]) => {
    try {
      await createRenewTask(domains);
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
    { title: "域名", dataIndex: "domain", ellipsis: true },
    { title: "Bucket", dataIndex: "bucket", ellipsis: true },
    { title: "Region", dataIndex: "region", ellipsis: true },
    { title: "云账号", dataIndex: "accountName", ellipsis: true },
    { title: "状态", dataIndex: "status", width: 90, render: (s: keyof typeof statusMap) => <Tag color={statusMap[s]?.color}>{statusMap[s]?.text ?? s}</Tag> },
    { title: "签发时间", dataIndex: "issuedAt", width: 160, render: (v: string) => v ? dayjs(v).format("YYYY-MM-DD HH:mm") : "-" },
    { title: "过期时间", dataIndex: "expiresAt", width: 160, sorter: (a, b) => dayjs(a.expiresAt).unix() - dayjs(b.expiresAt).unix(), render: (v: string) => v ? dayjs(v).format("YYYY-MM-DD HH:mm") : "-" },
    { title: "操作", width: 90, fixed: "right", render: (_, record) => (
      <Popconfirm title="确认为该域名创建续期任务？" onConfirm={() => handleRenew([record.domain])}><a><SyncOutlined /> 续期</a></Popconfirm>
    ) },
  ];

  const selectedDomains = data.filter((c) => selectedRowKeys.includes(c.id)).map((c) => c.domain);

  return (
    <Card title="证书列表" extra={
      <Space>
        <Input prefix={<SearchOutlined />} placeholder="搜索域名/Bucket/账号" allowClear value={search} onChange={(e) => setSearch(e.target.value)} style={{ width: 220 }} />
        <Select placeholder="状态" allowClear value={statusFilter} onChange={setStatusFilter} style={{ width: 120 }} options={[
          { value: "active", label: "正常" }, { value: "pending", label: "待处理" }, { value: "error", label: "异常" },
        ]} />
        <Button icon={<ReloadOutlined />} onClick={fetchData}>刷新</Button>
        {selectedRowKeys.length > 0 && (
          <Button type="primary" icon={<SyncOutlined />} onClick={() => handleRenew(selectedDomains)}>批量续期（SAN 证书）</Button>
        )}
      </Space>
    }>
      <Table<CertificateItem> rowKey="id" columns={columns} dataSource={filtered} loading={loading} pagination={{ pageSize: 20 }} size="middle"
        rowSelection={{ selectedRowKeys, onChange: setSelectedRowKeys }} scroll={{ x: 1000 }} />
    </Card>
  );
}
