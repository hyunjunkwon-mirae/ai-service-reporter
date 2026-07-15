// Mattermost webapp plugin registry 타입 정의 (필요한 것만).

export interface RhsRegistration {
  id: string;
  toggleRHSPlugin: { type: string };
  showRHSPlugin: { type: string };
  hideRHSPlugin: { type: string };
}

export interface PluginRegistry {
  // RHS 컴포넌트 등록 (필수)
  registerRightHandSidebarComponent(
    component: React.ComponentType,
    title: React.ReactNode
  ): RhsRegistration;

  // 채널 헤더 버튼
  registerChannelHeaderButtonAction?: (
    icon: React.ReactNode,
    action: () => void,
    dropdownText?: React.ReactNode,
    tooltipText?: React.ReactNode
  ) => string;

  // Apps Bar
  registerAppBarComponent?: (
    iconUrl: string,
    action: () => void,
    tooltipText: string
  ) => string;

  // System Console 의 커스텀 plugin setting (Mattermost 5.30+).
  // plugin.json 의 settings 항목 중 type: "custom" 인 키와 매칭됨.
  registerAdminConsoleCustomSetting?: (
    key: string,
    component: React.ComponentType<any>,
    options?: { showTitle?: boolean }
  ) => string;

  registerReducer?: (reducer: unknown) => void;
}
