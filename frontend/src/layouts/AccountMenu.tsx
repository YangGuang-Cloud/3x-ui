import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Dropdown } from 'antd';
import { DownOutlined, LogoutOutlined, UserOutlined } from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import './AccountMenu.css';

export default function AccountMenu() {
  const { t } = useTranslation();
  const [leaving, setLeaving] = useState(false);
  const username = document.querySelector<HTMLMetaElement>('meta[name="current-user"]')?.content;

  async function logout() {
    if (leaving) return;
    setLeaving(true);
    try {
      const result = await HttpUtil.post('/logout');
      if (result.success) window.location.assign(window.X_UI_BASE_PATH || '/');
    } finally {
      setLeaving(false);
    }
  }

  return (
    <div className="panel-account-menu">
      <Dropdown
        trigger={['click']}
        placement="bottomRight"
        menu={{
          items: [
            {
              key: 'logout',
              icon: <LogoutOutlined />,
              label: t('logout'),
              disabled: leaving,
              onClick: () => void logout(),
            },
          ],
        }}
      >
        <Button icon={<UserOutlined />} loading={leaving} aria-label={t('menu.account')}>
          <span className="panel-account-name">{username || t('menu.account')}</span>
          <DownOutlined />
        </Button>
      </Dropdown>
    </div>
  );
}
