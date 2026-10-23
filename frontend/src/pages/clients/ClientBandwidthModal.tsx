import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Form, InputNumber, Modal, Spin } from 'antd';
import { HttpUtil } from '@/utils';

type Limits = { uploadMbps: number; downloadMbps: number; supported: boolean };
export default function ClientBandwidthModal({
  email,
  onClose,
}: {
  email: string | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [limits, setLimits] = useState<Limits | null>(null);
  const [saving, setSaving] = useState(false);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let cancelled = false;
    if (email)
      void HttpUtil.get<Limits>(`/panel/api/clients/${encodeURIComponent(email)}/bandwidth`).then(
        (result) => {
          if (cancelled) return;
          if (result.success && result.obj) setLimits(result.obj);
          else setFailed(true);
        },
      );
    return () => {
      cancelled = true;
    };
  }, [email]);
  async function save() {
    if (!email || !limits || saving) return;
    setSaving(true);
    try {
      const result = await HttpUtil.post(
        `/panel/api/clients/${encodeURIComponent(email)}/bandwidth`,
        {
          uploadMbps: limits.uploadMbps,
          downloadMbps: limits.downloadMbps,
        },
      );
      if (result.success) onClose();
    } finally {
      setSaving(false);
    }
  }
  const unsupported = limits && !limits.supported;
  return (
    <Modal
      open={email !== null}
      title={`${t('pages.clients.bandwidthTitle')} · ${email || ''}`}
      onCancel={onClose}
      onOk={() => void save()}
      okText={t('save')}
      cancelText={t('cancel')}
      confirmLoading={saving}
      closable={!saving}
      mask={{ closable: !saving }}
      keyboard={!saving}
      cancelButtonProps={{ disabled: saving }}
      okButtonProps={{
        disabled: !limits || (!!unsupported && (limits.uploadMbps > 0 || limits.downloadMbps > 0)),
      }}
    >
      <Alert type="info" title={t('pages.clients.bandwidthHelp')} style={{ marginBottom: 16 }} />
      {unsupported && (
        <Alert
          type="warning"
          title={t('pages.clients.bandwidthUnsupported')}
          style={{ marginBottom: 16 }}
        />
      )}
      {failed ? (
        <Alert type="error" title={t('somethingWentWrong')} />
      ) : !limits ? (
        <Spin />
      ) : (
        <Form layout="vertical">
          <Form.Item label={t('pages.clients.bandwidthUpload')}>
            <InputNumber
              min={0}
              max={1000000}
              precision={2}
              step={1}
              value={limits.uploadMbps}
              disabled={saving}
              style={{ width: '100%' }}
              suffix="Mbps"
              aria-label={t('pages.clients.bandwidthUpload')}
              onChange={(v) => setLimits({ ...limits, uploadMbps: Number(v) || 0 })}
            />
          </Form.Item>
          <Form.Item label={t('pages.clients.bandwidthDownload')}>
            <InputNumber
              min={0}
              max={1000000}
              precision={2}
              step={1}
              value={limits.downloadMbps}
              disabled={saving}
              style={{ width: '100%' }}
              suffix="Mbps"
              aria-label={t('pages.clients.bandwidthDownload')}
              onChange={(v) => setLimits({ ...limits, downloadMbps: Number(v) || 0 })}
            />
          </Form.Item>
        </Form>
      )}
    </Modal>
  );
}
