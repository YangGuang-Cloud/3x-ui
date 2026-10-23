import { Outlet } from 'react-router';
import { ConfigProvider } from 'antd';

import { useWebSocketBridge } from '@/api/websocketBridge';
import { usePageTitle } from '@/hooks/usePageTitle';
import CommandPalette from '@/components/command-palette/CommandPalette';
import { useTheme } from '@/hooks/useTheme';
import AccountMenu from './AccountMenu';

export default function PanelLayout() {
  useWebSocketBridge();
  usePageTitle();
  const { antdThemeConfig } = useTheme();
  return (
    <>
      <ConfigProvider theme={antdThemeConfig}>
        <AccountMenu />
      </ConfigProvider>
      <Outlet />
      <CommandPalette />
    </>
  );
}
