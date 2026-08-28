import { useEffect, useState, useCallback } from "react";
import { Table, Tag, Button, App, Popconfirm, Card, Input, Select, Space, Tooltip } from "antd";
import {
  SyncOutlined,
  ReloadOutlined,
  SearchOutlined,
  StopOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  DeleteOutlined,
  DisconnectOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import {
  CertificateService,
  ListCertificatesRequestSchema,
  SetCertificateManagedRequestSchema,
  DeleteCertificateRequestSchema,
  type CertificateItem,
} from "@buf/certship_api.bufbuild_es/certship/v1/certificate_pb";
import {
  RenewTaskService,
  CreateRenewTaskRequestSchema,
} from "@buf/certship_api.bufbuild_es/certship/v1/renew_task_pb";
import { transport } from "@/api/transport";
import { useAutoRefresh, REFRESH_OPTIONS } from "@/hooks/useAutoRefresh";
import { formatError } from "@/utils/error";

const statusMap = {
  active: { color: "success" as const, text: "正常" },
  pending: { color: "processing" as const, text: "待处理" },
  error: { color: "error" as const, text: "异常" },
};

// 云上存在性与证书状态是两回事：域名可能证书还没过期，但已经从阿里云上摘掉了
const presenceMap = {
  present: { color: "green" as const, text: "在云上" },
  missing: { color: "orange" as const, text: "已消失" },
  archived: { color: "default" as const, text: "已归档" },
};

const errorKindMap: Record<string, string> = {
  transient: "可重试错误",
  permanent: "需人工介入",
  rate_limited: "被限速",
};

export default function Certificates() {
  const { message } = App.useApp();
  const [data, setData] = useState<CertificateItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<string | null>(null);
  const [presenceFilter, setPresenceFilter] = useState<string | null>(null);

  const fetchData = useCallback(() => {
    setLoading(true);
    const client = createClient(CertificateService, transport);
    client
      .listCertificates(create(ListCertificatesRequestSchema, { limit: BigInt(100) }))
      .then((res) => setData([...res.certificates]))
      .catch((err) => message.error(`加载证书列表失败:${formatError(err)}`))
      .finally(() => setLoading(false));
  }, [message]);

  const { interval: refreshInterval, setInterval: setRefreshInterval } = useAutoRefresh(fetchData);
  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const handleRenew = async (domains: string[]) => {
    const client = createClient(RenewTaskService, transport);
    const results = await Promise.allSettled(
      domains.map((domain) =>
        client.createRenewTask(create(CreateRenewTaskRequestSchema, { domain })),
      ),
    );
    const ok = results.filter((r) => r.status === "fulfilled").length;
    const failed = results.length - ok;
    if (failed === 0) {
      message.success(`已创建 ${ok} 个续期任务`);
    } else if (ok === 0) {
      message.error(
        `创建失败:${(results[0] as PromiseRejectedResult).reason?.message ?? "未知错误"}`,
      );
    } else {
      message.warning(`创建 ${ok} 个,失败 ${failed} 个`);
    }
    setSelectedRowKeys([]);
    fetchData();
  };

  const handleSetManaged = async (ids: string[], managed: boolean) => {
    const client = createClient(CertificateService, transport);
    const results = await Promise.allSettled(
      ids.map((id) =>
        client.setCertificateManaged(create(SetCertificateManagedRequestSchema, { id, managed })),
      ),
    );
    const ok = results.filter((r) => r.status === "fulfilled").length;
    const action = managed ? "恢复托管" : "暂停托管";
    if (ok === results.length) {
      message.success(`已${action} ${ok} 个域名`);
    } else if (ok === 0) {
      message.error(`${action}失败:${formatError((results[0] as PromiseRejectedResult).reason)}`);
    } else {
      message.warning(`${action} ${ok} 个,失败 ${results.length - ok} 个`);
    }
    setSelectedRowKeys([]);
    fetchData();
  };

  const handleDelete = async (record: CertificateItem) => {
    const client = createClient(CertificateService, transport);
    try {
      await client.deleteCertificate(create(DeleteCertificateRequestSchema, { id: record.id }));
      message.success(`已删除 ${record.domain}`);
      fetchData();
    } catch (err) {
      message.error(`删除失败:${formatError(err)}`);
    }
  };

  const filtered = data.filter((c) => {
    if (statusFilter && c.status !== statusFilter) return false;
    if (presenceFilter && c.presence !== presenceFilter) return false;
    if (
      search &&
      !c.domain.includes(search) &&
      !c.bucket.includes(search) &&
      !c.accountName.includes(search)
    )
      return false;
    return true;
  });

  const columns: ColumnsType<CertificateItem> = [
    {
      title: "域名",
      dataIndex: "domain",
      width: 280,
      render: (v: string, record: CertificateItem) => (
        <Space size={4}>
          <span>{v}</span>
          {!record.managed && (
            <Tooltip title="已暂停托管,certship 不会为它签发或续期">
              <Tag color="default" icon={<PauseCircleOutlined />} style={{ margin: 0 }}>
                暂停
              </Tag>
            </Tooltip>
          )}
          {record.blockedReason && (
            <Tooltip title={record.blockedReason}>
              <Tag color="volcano" icon={<StopOutlined />} style={{ margin: 0 }}>
                阻塞
              </Tag>
            </Tooltip>
          )}
        </Space>
      ),
    },
    {
      title: "Bucket",
      dataIndex: "bucket",
      width: 190,
      ellipsis: true,
      render: (v: string) => v || <span style={{ color: "#999" }}>-</span>,
    },
    {
      title: "部署",
      dataIndex: "deployTarget",
      width: 70,
      align: "center",
      render: (v: string, record: CertificateItem) => (
        <Tooltip
          title={`发现来源:${record.origin === "both" ? "OSS + CDN" : record.origin?.toUpperCase()}`}
        >
          <Tag color={v === "cdn" ? "orange" : "blue"}>{v === "cdn" ? "CDN" : "OSS"}</Tag>
        </Tooltip>
      ),
    },
    {
      title: "云上",
      dataIndex: "presence",
      width: 80,
      align: "center",
      render: (p: string, record: CertificateItem) => {
        const m = presenceMap[p as keyof typeof presenceMap];
        const seen = record.lastSeenAt
          ? `最后可见:${dayjs(record.lastSeenAt).format("YYYY-MM-DD HH:mm")}`
          : "尚未扫描到";
        return m ? (
          <Tooltip title={seen}>
            <Tag color={m.color} icon={p === "present" ? undefined : <DisconnectOutlined />}>
              {m.text}
            </Tag>
          </Tooltip>
        ) : (
          p
        );
      },
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 70,
      align: "center",
      render: (s: string, record: CertificateItem) => {
        const m = statusMap[s as keyof typeof statusMap];
        const tag = m ? <Tag color={m.color}>{m.text}</Tag> : <>{s}</>;
        if (s !== "error") return tag;
        const kind = errorKindMap[record.errorKind] ?? record.errorKind;
        const retry = record.nextRetryAt
          ? `,${dayjs(record.nextRetryAt).format("MM-DD HH:mm")} 后重试`
          : "";
        return (
          <Tooltip
            title={`${kind}(已失败 ${record.retryCount} 次${retry})\n${record.errorMessage}`}
          >
            {tag}
          </Tooltip>
        );
      },
    },
    {
      title: "过期时间",
      dataIndex: "expiresAt",
      width: 155,
      render: (v: string) => (v ? dayjs(v).format("YYYY-MM-DD HH:mm") : "-"),
    },
    {
      title: "剩余",
      width: 80,
      align: "center",
      render: (_: unknown, record: CertificateItem) => {
        if (!record.expiresAt) return "-";
        const days = dayjs(record.expiresAt).diff(dayjs(), "day");
        const color = days <= 0 ? "red" : days <= 7 ? "red" : days <= 30 ? "orange" : "green";
        return <Tag color={color}>{days <= 0 ? "已过期" : `${days}天`}</Tag>;
      },
    },
    {
      title: "操作",
      width: 150,
      align: "center",
      render: (_, record) => (
        <Space size={8}>
          <Popconfirm title="确认续期？" onConfirm={() => handleRenew([record.domain])}>
            <a>
              <SyncOutlined /> 续期
            </a>
          </Popconfirm>
          {record.managed ? (
            <Popconfirm
              title="暂停托管后不再自动签发续期,记录会保留"
              onConfirm={() => handleSetManaged([record.id], false)}
            >
              <a>
                <PauseCircleOutlined /> 暂停
              </a>
            </Popconfirm>
          ) : (
            <a onClick={() => void handleSetManaged([record.id], true)}>
              <PlayCircleOutlined /> 恢复
            </a>
          )}
          {record.presence === "archived" && (
            <Popconfirm title="删除这条已归档的记录？" onConfirm={() => handleDelete(record)}>
              <a style={{ color: "#ff4d4f" }}>
                <DeleteOutlined />
              </a>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  const selectedRows = data.filter((c) => selectedRowKeys.includes(c.id));
  const selectedDomains = selectedRows.map((c) => c.domain);
  const selectedIds = selectedRows.map((c) => c.id);

  return (
    <Card
      title="证书列表"
      extra={
        <Space>
          <Input
            prefix={<SearchOutlined />}
            placeholder="搜索"
            allowClear
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            style={{ width: 160 }}
          />
          <Select
            placeholder="状态"
            allowClear
            value={statusFilter}
            onChange={setStatusFilter}
            style={{ width: 100 }}
            options={[
              { value: "active", label: "正常" },
              { value: "pending", label: "待处理" },
              { value: "error", label: "异常" },
            ]}
          />
          <Select
            placeholder="云上"
            allowClear
            value={presenceFilter}
            onChange={setPresenceFilter}
            style={{ width: 110 }}
            options={[
              { value: "present", label: "在云上" },
              { value: "missing", label: "已消失" },
              { value: "archived", label: "已归档" },
            ]}
          />
          <Select
            value={refreshInterval}
            onChange={setRefreshInterval}
            style={{ width: 140 }}
            options={REFRESH_OPTIONS}
          />
          <Button icon={<ReloadOutlined />} onClick={fetchData}>
            刷新
          </Button>
          {selectedRowKeys.length > 0 && (
            <>
              <Button
                type="primary"
                icon={<SyncOutlined />}
                onClick={() => void handleRenew(selectedDomains)}
              >
                批量续期
              </Button>
              <Popconfirm
                title={`暂停托管选中的 ${selectedIds.length} 个域名？`}
                onConfirm={() => handleSetManaged(selectedIds, false)}
              >
                <Button icon={<PauseCircleOutlined />}>批量暂停</Button>
              </Popconfirm>
            </>
          )}
        </Space>
      }
    >
      <Table<CertificateItem>
        rowKey="id"
        columns={columns}
        dataSource={filtered}
        loading={loading}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 条` }}
        size="middle"
        rowSelection={{ selectedRowKeys, onChange: setSelectedRowKeys }}
        scroll={{ x: 1040 }}
      />
    </Card>
  );
}
