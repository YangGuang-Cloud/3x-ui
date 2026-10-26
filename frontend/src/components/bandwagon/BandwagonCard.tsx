import { usagePercent, speedPercent } from './usage';
import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Col,
  Form,
  Input,
  InputNumber,
  Modal,
  Progress,
  Row,
  Space,
  Statistic,
  Switch,
  Typography,
  Tooltip,
} from 'antd';
import { HttpUtil, SizeFormatter } from '@/utils';

type ProviderConfig = {
  enabled: boolean;
  veid: string;
  apiKey?: string;
  uploadMbps: number;
  downloadMbps: number;
};
type Cycle = {
  id: number;
  start: number;
  nextReset: number;
  partial: boolean;
  quota: number;
  used: number;
  updatedAt: number;
};
export type ProviderStatus = {
  config: ProviderConfig;
  hasKey: boolean;
  cycle: Cycle | null;
  history: Cycle[];
  clients: { clientId: number; up: number; down: number }[];
  presence?: { clientId: number; onlineSeconds: number; onlineIPs: number }[];
  presenceAvailable?: boolean;
  total: number;
  stale: boolean;
  awaitingReset: boolean;
  error: string;
};
export function useBandwagon() {
  return useQuery({
    queryKey: ['bandwagon'],
    queryFn: async () => {
      const r = await HttpUtil.get<ProviderStatus>('/panel/api/bandwagon', undefined, {
        silent: true,
      });
      if (!r.success || !r.obj) throw new Error(r.msg || '搬瓦工统计读取失败');
      return r.obj;
    },
    refetchInterval: 15000,
    staleTime: 5000,
    retry: false,
  });
}
function percentText(value: number | null) {
  return value === null ? '—' : value > 0 && value < 0.01 ? '<0.01%' : `${value.toFixed(2)}%`;
}
const date = (unix: number) => new Date(unix * 1000).toLocaleString();

export function ClientProviderUsage({
  status,
  clientId,
  speed,
  metric,
}: {
  status?: ProviderStatus;
  clientId: number;
  speed?: { up: number; down: number };
  metric: 'traffic' | 'share' | 'bandwidth';
}) {
  if (metric === 'bandwidth') {
    return (
      <Tooltip title="实时上传 / 下载速率 ÷ 对应的标称带宽；未设置标称带宽时显示 —">
        <span>
          ↑ {percentText(speed ? speedPercent(speed.up, status?.config.uploadMbps || 0) : null)}
          {' / '}↓{' '}
          {percentText(speed ? speedPercent(speed.down, status?.config.downloadMbps || 0) : null)}
        </span>
      </Tooltip>
    );
  }
  if (!status?.config.enabled || !status.cycle) return <span>—</span>;
  const row = status.clients.find((c) => c.clientId === clientId);
  const used = (row?.up || 0) + (row?.down || 0);
  if (metric === 'share') {
    return (
      <Tooltip title="该客户端的周期流量 ÷ 搬瓦工套餐流量额度">
        <span>{percentText(usagePercent(used, status.cycle.quota))}</span>
      </Tooltip>
    );
  }
  return (
    <Tooltip
      title={`上传 ${SizeFormatter.sizeFormat(row?.up || 0)} / 下载 ${SizeFormatter.sizeFormat(row?.down || 0)}`}
    >
      <span>{SizeFormatter.sizeFormat(used)}</span>
    </Tooltip>
  );
}

export default function BandwagonCard() {
  const { data, error, refetch } = useBandwagon();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [history, setHistory] = useState(false);
  const [failure, setFailure] = useState('');
  const [form] = Form.useForm<ProviderConfig>();
  async function sync() {
    setBusy(true);
    setFailure('');
    try {
      const r = await HttpUtil.post('/panel/api/bandwagon/sync', {});
      if (!r.success) setFailure(r.msg || '同步失败');
      await refetch();
    } catch {
      setFailure('同步失败，请稍后重试');
    } finally {
      setBusy(false);
    }
  }
  async function save() {
    const value = await form.validateFields();
    setBusy(true);
    setFailure('');
    try {
      const r = await HttpUtil.post('/panel/api/bandwagon/config', value, {
        headers: { 'Content-Type': 'application/json' },
      });
      if (!r.success) {
        setFailure(r.msg || '保存失败');
        return;
      }
      setOpen(false);
      form.setFieldValue('apiKey', '');
      await sync();
    } catch {
      setFailure('保存失败，请稍后重试');
    } finally {
      setBusy(false);
    }
  }
  const cycle = data?.cycle;
  return (
    <Card
      size="small"
      title="搬瓦工 · 本周期流量"
      extra={
        <Space wrap>
          <Button
            size="small"
            disabled={!data?.config.enabled}
            loading={busy}
            onClick={() => void sync()}
          >
            同步
          </Button>
          <Button
            size="small"
            onClick={() => {
              setFailure('');
              form.setFieldsValue(
                data?.config || { enabled: true, veid: '', uploadMbps: 0, downloadMbps: 0 },
              );
              form.setFieldValue('apiKey', '');
              setOpen(true);
            }}
          >
            统计设置
          </Button>
        </Space>
      }
    >
      <Space orientation="vertical" style={{ width: '100%' }} size={12}>
        {(failure || error || data?.error) && (
          <Alert type="warning" title={failure || error?.message || data?.error} />
        )}
        {!data?.config.enabled ? (
          <Typography.Text type="secondary">
            连接 KiwiVM 后显示套餐用量，并按供应商账期自动切换本地统计。
          </Typography.Text>
        ) : !cycle ? (
          <Alert type="info" title="尚未取得搬瓦工数据，请同步。" />
        ) : (
          <>
            {(data.stale || data.awaitingReset) && (
              <Alert
                type="warning"
                title={
                  data.awaitingReset
                    ? '已到重置时间，等待搬瓦工确认新账期；暂不清零。'
                    : '供应商数据已过期，以下为上次成功同步值。'
                }
              />
            )}
            <Row gutter={[16, 12]}>
              <Col xs={24} md={8}>
                <Statistic
                  title="服务器已用 / 套餐额度"
                  value={`${SizeFormatter.sizeFormat(cycle.used)} / ${SizeFormatter.sizeFormat(cycle.quota)}`}
                  styles={{ content: { fontSize: 20 } }}
                />
                <Progress
                  percent={Math.min(100, usagePercent(cycle.used, cycle.quota) || 0)}
                  format={() => percentText(usagePercent(cycle.used, cycle.quota))}
                />
              </Col>
              <Col xs={12} md={8}>
                <Statistic
                  title="服务器剩余流量"
                  value={SizeFormatter.sizeFormat(Math.max(0, cycle.quota - cycle.used))}
                  styles={{ content: { fontSize: 20 } }}
                />
              </Col>
              <Col xs={12} md={8}>
                <Statistic
                  title="本面板本周期客户端总计"
                  value={SizeFormatter.sizeFormat(data.total)}
                  styles={{ content: { fontSize: 20 } }}
                />
              </Col>
            </Row>
            <Typography.Text>
              下次重置：{date(cycle.nextReset)} · 同步时间：{date(cycle.updatedAt)}
            </Typography.Text>
            <Typography.Text type="secondary">
              供应商总量包含其他业务与协议开销；客户端占比为估算。本机客户端配额按同一账期用量计算。
            </Typography.Text>
            <Typography.Text type="secondary">
              客户端上传标称上限：{data.config.uploadMbps || '未设置'} Mbps · 客户端下载标称上限：
              {data.config.downloadMbps || '未设置'} Mbps（参考值，不代表实时可用容量）
            </Typography.Text>
            <Button type="link" size="small" onClick={() => setHistory(!history)}>
              {history ? '收起历史账期' : '历史账期'}
            </Button>
            {history &&
              data.history.slice(1).map((c) => (
                <Typography.Text key={c.id}>
                  {date(c.start)} — {date(c.nextReset)}：供应商最后记录{' '}
                  {SizeFormatter.sizeFormat(c.used)} / {SizeFormatter.sizeFormat(c.quota)}
                  {c.partial ? '（不完整周期）' : ''}
                </Typography.Text>
              ))}
            {history && data.history.length < 2 && (
              <Typography.Text type="secondary">尚无历史账期</Typography.Text>
            )}
          </>
        )}
      </Space>
      <Modal
        title="搬瓦工统计设置"
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => void save()}
        confirmLoading={busy}
      >
        <Form form={form} layout="vertical">
          {failure && <Alert type="error" title={failure} />}
          <Form.Item name="enabled" label="启用统计" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="veid" label="VEID">
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item
            name="apiKey"
            label="API Key"
            extra={
              data?.hasKey
                ? '已保存，留空保持不变。密钥只保存在后台。'
                : '从 KiwiVM 的 API 页面获取。'
            }
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="uploadMbps"
            label="客户端上传方向标称带宽（Mbps）"
            extra="对应服务器入站方向；未知填 0。"
          >
            <InputNumber min={0} max={1000000} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            name="downloadMbps"
            label="客户端下载方向标称带宽（Mbps）"
            extra="对应服务器出站方向；1 Gbps = 1000 Mbps。"
          >
            <InputNumber min={0} max={1000000} style={{ width: '100%' }} />
          </Form.Item>
          <Alert
            type="info"
            title="每 5 分钟读取一次供应商数据；确认账期变化后自动切换周期统计，保留历史，本机客户端配额随确认的新账期恢复。"
          />
        </Form>
      </Modal>
    </Card>
  );
}

export function ClientPresenceMetric({
  status,
  clientId,
  metric,
}: {
  status?: ProviderStatus;
  clientId: number;
  metric: 'duration' | 'ips';
}) {
  const row = status?.presence?.find((c) => c.clientId === clientId);
  if (metric === 'ips')
    return (
      <Tooltip title="核心当前观测到的来源 IP 数；同一网络的多台设备可能共用一个 IP。">
        <span>{status?.presenceAvailable ? (row?.onlineIPs ?? 0) : '—'}</span>
      </Tooltip>
    );
  if (!status?.config.enabled || !status.cycle) return <span>—</span>;
  const seconds = row?.onlineSeconds ?? 0;
  const hours = Math.floor(seconds / 3600),
    minutes = Math.floor((seconds % 3600) / 60),
    rest = seconds % 60;
  return (
    <Tooltip title="搬瓦工本账期累计在线时长，按账号计算，不按 IP 数叠加；按检测结果累计，离线或检测中断时暂停。">
      <span>
        {hours}小时 {minutes}分 {rest}秒
      </span>
    </Tooltip>
  );
}
