import { useEffect, useState } from "react";
import { Table, Tag, Card, Select, Space, Tooltip, Button } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { listRenewTasks, type RenewTaskItem } from "@/api/renew-task";

const statusMap = { pending: { color: "default" as const, text: "待执行" }, running: { color: "processing" as const, text: "执行中" }, success: { color: "success" as const, text: "成功" }, failed: { color: "error" as const, text: "失败" } };
const triggerMap = { auto: { color: "blue" as const, text: "自动" }, manual: { color: "orange" as const, text: "手动" } };

export default function RenewTasks() {
  const [data, setData] = useState<RenewTaskItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState<string | null>(null);

  const fetchData = () => { setLoading(true); listRenewTasks().then((res) => setData(res.tasks ?? [])).finally(() => setLoading(false)); };
  useEffect(() => { fetchData(); }, []);

  const filtered = statusFilter ? data.filter((t) => t.status === statusFilter) : data;

  const columns: ColumnsType<RenewTaskItem> = [
    { title: "域名", dataIndex: "domains", render: (domains: string[]) => <Tooltip title={domains.join("\n")}><span>{domains.join(", ")}</span></Tooltip> },
    { title: "状态", dataIndex: "status", width: 90, render: (s: keyof typeof statusMap) => <Tag color={statusMap[s]?.color}>{statusMap[s]?.text ?? s}</Tag> },
    { title: "触发方式", dataIndex: "trigger", width: 90, render: (t: keyof typeof triggerMap) => <Tag color={triggerMap[t]?.color}>{triggerMap[t]?.text ?? t}</Tag> },
    { title: "错误信息", dataIndex: "errorMessage", ellipsis: true, render: (v: string) => v || "-" },
    { title: "开始时间", dataIndex: "startedAt", width: 170, render: (v: string) => v ? dayjs(v).format("YYYY-MM-DD HH:mm:ss") : "-" },
    { title: "完成时间", dataIndex: "finishedAt", width: 170, render: (v: string) => v ? dayjs(v).format("YYYY-MM-DD HH:mm:ss") : "-" },
    { title: "创建时间", dataIndex: "createdAt", width: 170, sorter: (a, b) => dayjs(a.createdAt).unix() - dayjs(b.createdAt).unix(), defaultSortOrder: "descend", render: (v: string) => dayjs(v).format("YYYY-MM-DD HH:mm:ss") },
  ];

  return (
    <Card title="续期任务" extra={<Space>
      <Select placeholder="状态" allowClear value={statusFilter} onChange={setStatusFilter} style={{ width: 120 }} options={[
        { value: "pending", label: "待执行" }, { value: "running", label: "执行中" }, { value: "success", label: "成功" }, { value: "failed", label: "失败" },
      ]} />
      <Button icon={<ReloadOutlined />} onClick={fetchData}>刷新</Button>
    </Space>}>
      <Table<RenewTaskItem> rowKey="id" columns={columns} dataSource={filtered} loading={loading} pagination={{ pageSize: 20 }} size="middle" />
    </Card>
  );
}
