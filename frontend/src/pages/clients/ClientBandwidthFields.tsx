import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Col, Form, InputNumber, Row, Spin } from 'antd';
import { HttpUtil } from '@/utils';

export type ClientLimits = { uploadMbps: number; downloadMbps: number; supported: boolean };
const validRate = (n: number) => Number.isFinite(n) && (n === 0 || (n >= 0.01 && n <= 1000000));

export function useClientBandwidthDraft(email: string | null) {
  const [initial, setInitial] = useState<ClientLimits | null>(null);
  const [value, setValue] = useState<ClientLimits | null>(null);
  const [failed, setFailed] = useState(false);
  const [revision, setRevision] = useState(0);
  const [loadedEmail, setLoadedEmail] = useState<string | null>(null);
  const requestKey = `${email ?? ''}:${revision}`;
  const [draftKey, setDraftKey] = useState(requestKey);
  if (draftKey !== requestKey) {
    setDraftKey(requestKey);
    setInitial(null);
    setValue(null);
    setFailed(false);
    setLoadedEmail(null);
  }
  useEffect(() => {
    let cancelled = false;
    if (email) {
      void HttpUtil.get<ClientLimits>(`/panel/api/clients/${encodeURIComponent(email)}/bandwidth`)
        .then((result) => {
          if (cancelled) return;
          if (
            result?.success &&
            result.obj &&
            validRate(result.obj.uploadMbps) &&
            validRate(result.obj.downloadMbps)
          ) {
            setInitial(result.obj);
            setValue(result.obj);
            setLoadedEmail(email);
          } else setFailed(true);
        })
        .catch(() => {
          if (!cancelled) setFailed(true);
        });
    }
    return () => {
      cancelled = true;
    };
  }, [email, revision]);
  const ready = !!value && loadedEmail === email;
  const changed =
    ready &&
    !!initial &&
    (value.uploadMbps !== initial.uploadMbps || value.downloadMbps !== initial.downloadMbps);
  const canSave =
    !email ||
    (ready &&
      validRate(value.uploadMbps) &&
      validRate(value.downloadMbps) &&
      (!changed || value.supported || (value.uploadMbps === 0 && value.downloadMbps === 0)));
  return {
    value,
    setValue,
    failed,
    ready,
    changed,
    canSave,
    retry: () => setRevision((r) => r + 1),
  };
}

export default function ClientBandwidthFields({
  draft,
  disabled,
}: {
  draft: ReturnType<typeof useClientBandwidthDraft>;
  disabled: boolean;
}) {
  const { t } = useTranslation();
  const { value, failed, ready, setValue } = draft;
  return (
    <>
      <Alert type="info" title={t('pages.clients.bandwidthHelp')} style={{ marginBottom: 16 }} />

      {failed ? (
        <Alert
          type="error"
          title={t('pages.clients.bandwidthLoadFailed')}
          action={
            <Button disabled={disabled} onClick={draft.retry}>
              {t('refresh')}
            </Button>
          }
        />
      ) : !ready || !value ? (
        <Spin />
      ) : (
        <>
          {!value.supported && (
            <Alert
              type="warning"
              title={t('pages.clients.bandwidthUnsupported')}
              style={{ marginBottom: 16 }}
            />
          )}
          <Row gutter={16}>
            {(['uploadMbps', 'downloadMbps'] as const).map((field) => {
              const label = t(
                field === 'uploadMbps'
                  ? 'pages.clients.bandwidthUpload'
                  : 'pages.clients.bandwidthDownload',
              );
              return (
                <Col xs={24} md={12} key={field}>
                  <Form.Item label={label}>
                    <InputNumber
                      min={0}
                      max={1000000}
                      precision={2}
                      step={1}
                      value={value[field]}
                      disabled={disabled}
                      style={{ width: '100%' }}
                      suffix="Mbps"
                      aria-label={label}
                      onChange={(v) => setValue({ ...value, [field]: Number(v) || 0 })}
                    />
                  </Form.Item>
                </Col>
              );
            })}
          </Row>
        </>
      )}
    </>
  );
}
