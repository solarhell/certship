import { useEffect, useState } from "react";
import { Card, Col, Row, Statistic, Table, Tag, Typography } from "antd";
import { SafetyCertificateOutlined, CheckCircleOutlined, ClockCircleOutlined, ExclamationCircleOutlined, CloudOutlined, DatabaseOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { createClient } from "@connectrpc/connect";
import { create } from "@bufbuild/protobuf";
import { CertificateService, ListCertificatesRequestSchema, type CertificateItem } from "@buf/wolotec_certship.bufbuild_es/certship/v1/certificate_pb";
import { transport } from "@/api/transport";

const statusMap = {
  active: { color: "success" as const, text: "正常" },
  pending: { color: "processing" as const, text: "待处理" },
  error: { color: "error" as const, text: "异常" },
};

export default function Dashboard() {
  const [certs, setCerts] = useState<CertificateItem[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const client = createClient(CertificateService, transport);
    client.listCertificates(create(ListCertificatesRequestSchema, { limit: BigInt(100) }))
      .then((res) => setCerts([...res.certificates]))
      .finally(() => setLoading(false));
  }, []);

  const total = certs.length;
  const active = certs.filter((c) => c.status === "active").length;
  const pending = certs.filter((c) => c.status === "pending").length;
  const error = certs.filter((c) => c.status === "error").length;
  const cdnCount = certs.filter((c) => c.deployTarget === "cdn").length;
  const ossCount = certs.filter((c) => c.deployTarget !== "cdn").length;

  const expiringSoon = certs
    .filter((c) => c.status === "active" && c.expiresAt)
    .filter((c) => dayjs(c.expiresAt).diff(dayjs(), "day") <= 30)
    .sort((a, b) => dayjs(a.expiresAt).unix() - dayjs(b.expiresAt).unix());

  const columns: ColumnsType<CertificateItem> = [
    { title: "域名", dataIndex: "domain" },
    { title: "部署", dataIndex: "deployTarget", width: 80, render: (v: string) => <Tag color={v === "cdn" ? "orange" : "blue"}>{v === "cdn" ? "CDN" : "OSS"}</Tag> },
    { title: "Bucket", dataIndex: "bucket" },
    { title: "云账号", dataIndex: "accountName" },
    { title: "过期时间", dataIndex: "expiresAt", render: (v: string) => {
      const days = dayjs(v).diff(dayjs(), "day");
      return <Tag color={days <= 7 ? "error" : "warning"}>{dayjs(v).format("YYYY-MM-DD")}（{days}天后）</Tag>;
    } },
    { title: "状态", dataIndex: "status", render: (s: string) => { const m = statusMap[s as keyof typeof statusMap]; return m ? <Tag color={m.color}>{m.text}</Tag> : s; } },
  ];

  return (
    <div>
      <Row gutter={[16, 16]}>
        <Col xs={12} sm={6}><Card><Statistic title="总域名数" value={total} prefix={<SafetyCertificateOutlined />} /></Card></Col>
        <Col xs={12} sm={6}><Card><Statistic title="活跃证书" value={active} prefix={<CheckCircleOutlined />} valueStyle={{ color: "#52c41a" }} /></Card></Col>
        <Col xs={12} sm={6}><Card><Statistic title="待处理" value={pending} prefix={<ClockCircleOutlined />} valueStyle={{ color: "#1677ff" }} /></Card></Col>
        <Col xs={12} sm={6}><Card><Statistic title="异常" value={error} prefix={<ExclamationCircleOutlined />} valueStyle={{ color: "#ff4d4f" }} /></Card></Col>
        <Col xs={12} sm={6}><Card><Statistic title="CDN 域名" value={cdnCount} prefix={<CloudOutlined />} valueStyle={{ color: "#fa8c16" }} /></Card></Col>
        <Col xs={12} sm={6}><Card><Statistic title="OSS 直连" value={ossCount} prefix={<DatabaseOutlined />} valueStyle={{ color: "#1677ff" }} /></Card></Col>
      </Row>
      <Card style={{ marginTop: 16 }} title={<Typography.Text strong>即将过期证书（30 天内）</Typography.Text>} loading={loading}>
        {expiringSoon.length === 0
          ? <Typography.Text type="secondary">暂无即将过期的证书</Typography.Text>
          : <Table rowKey="id" dataSource={expiringSoon} columns={columns} pagination={false} size="small" />}
      </Card>
    </div>
  );
}
