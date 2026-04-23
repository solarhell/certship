import { useEffect, useState, useCallback } from "react";
import { Table, Tag, Card, Select, Space, Tooltip, Button, Modal, Timeline } from "antd";
import { ReloadOutlined, FileTextOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { RenewTaskService, ListRenewTasksRequestSchema, type RenewTaskItem } from "@buf/wolotec_certship.bufbuild_es/certship/v1/renew_task_pb";
import { transport } from "@/api/transport";
import { useAutoRefresh, REFRESH_OPTIONS } from "@/hooks/useAutoRefresh";

const statusMap = { pending: { color: "default" as const, text: "待执行" }, running: { color: "processing" as const, text: "执行中" }, success: { color: "success" as const, text: "成功" }, failed: { color: "error" as const, text: "失败" } };
const triggerMap = { auto: { color: "blue" as const, text: "自动" }, manual: { color: "orange" as const, text: "手动" } };

export default function RenewTasks() {
  const [data, setData] = useState<RenewTaskItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState<string | null>(null);
  const [logTask, setLogTask] = useState<RenewTaskItem | null>(null);

  const fetchData = useCallback(() => {
    setLoading(true);
    const client = createClient(RenewTaskService, transport);
    client.listRenewTasks(create(ListRenewTasksRequestSchema, { limit: BigInt(100) }))
      .then((res) => setData([...res.tasks]))
      .finally(() => setLoading(false));
  }, []);

  const { interval: refreshInterval, setInterval: setRefreshInterval } = useAutoRefresh(fetchData);
  useEffect(() => { fetchData(); }, [fetchData]);

  const filtered = statusFilter ? data.filter((t) => t.status === statusFilter) : data;

  const columns: ColumnsType<RenewTaskItem> = [
    { title: "域名", dataIndex: "domains", width: 280, render: (_, r) => (
      <Tooltip title={r.domains.join("\n")}><span>{r.domains.join(", ")}</span></Tooltip>
    ) },
    { title: "状态", width: 70, align: "center", dataIndex: "status", render: (s: string) => { const m = statusMap[s as keyof typeof statusMap]; return m ? <Tag color={m.color}>{m.text}</Tag> : s; } },
    { title: "触发", width: 60, align: "center", dataIndex: "trigger", render: (t: string) => { const m = triggerMap[t as keyof typeof triggerMap]; return m ? <Tag color={m.color}>{m.text}</Tag> : t; } },
    { title: "完成时间", width: 175, render: (_: unknown, r: RenewTaskItem) => {
      if (!r.finishedAt) return "-";
      const text = dayjs(r.finishedAt).format("YYYY-MM-DD HH:mm");
      if (!r.startedAt) return text;
      const sec = dayjs(r.finishedAt).diff(dayjs(r.startedAt), "second");
      const dur = sec < 60 ? `${sec}s` : `${Math.floor(sec / 60)}m${sec % 60}s`;
      return <>{text} <Tag style={{ marginLeft: 4 }}>{dur}</Tag></>;
    } },
    { title: "错误信息", dataIndex: "errorMessage", ellipsis: true, render: (v: string) => v ? <Tooltip title={v}><Tag color="error">失败</Tag></Tooltip> : "-" },
    { title: "创建时间", dataIndex: "createdAt", width: 155, render: (v: string) => dayjs(v).format("YYYY-MM-DD HH:mm") },
    { title: "日志", width: 60, align: "center", render: (_, record) => (
      record.log.length > 0
        ? <a onClick={() => setLogTask(record)}><FileTextOutlined /></a>
        : <span style={{ color: "#ccc" }}><FileTextOutlined /></span>
    ) },
  ];

  return (
    <>
      <Card title="续期任务" extra={<Space>
        <Select placeholder="状态" allowClear value={statusFilter} onChange={setStatusFilter} style={{ width: 100 }} options={[
          { value: "pending", label: "待执行" }, { value: "running", label: "执行中" }, { value: "success", label: "成功" }, { value: "failed", label: "失败" },
        ]} />
        <Select value={refreshInterval} onChange={setRefreshInterval} style={{ width: 140 }} options={REFRESH_OPTIONS} />
        <Button icon={<ReloadOutlined />} onClick={fetchData}>刷新</Button>
      </Space>}>
        <Table<RenewTaskItem> rowKey="id" columns={columns} dataSource={filtered} loading={loading}
          pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 条` }} size="middle" scroll={{ x: 800 }} />
      </Card>

      <Modal
        title={`任务日志 - ${logTask?.domains.join(", ") ?? ""}`}
        open={!!logTask}
        onCancel={() => setLogTask(null)}
        footer={null}
        width={640}
      >
        {logTask && (
          <Timeline
            style={{ marginTop: 16 }}
            items={logTask.log.map((entry) => ({
              color: entry.content.startsWith("✅") ? "green" : entry.content.startsWith("❌") ? "red" : entry.content.startsWith("⚠") ? "orange" : "blue",
              children: (
                <div>
                  <span style={{ color: "#999", marginRight: 8, fontSize: 12 }}>{dayjs(entry.time).format("HH:mm:ss")}</span>
                  {entry.content}
                </div>
              ),
            }))}
          />
        )}
      </Modal>
    </>
  );
}
