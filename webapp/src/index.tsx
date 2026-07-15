// =============================================================================
// AI Service Reporter Plugin — Webapp Entry
//
// 등록 진입점:
//   1) RHS 컴포넌트 — 사용자 구독 설정 패널
//   2) 채널 헤더 버튼 — 모든 버전에서 동작하는 진입점 (필수)
//   3) Apps Bar 아이콘 — 7.x+ 환경에서 표시 (보너스)
//   4) System Console custom setting — admin 의 업무영역 코드 관리 UI
//
// 1~3 은 가시성 사전 체크 통과 시에만 등록 (VisibleToUsers + isAdmin 고려).
// 4 는 가시성과 무관하게 시스템 관리자만 보는 페이지이므로 항상 등록.
// =============================================================================

import React from "react";

import RhsPanel from "./components/rhs";
import ResourceGroupAdminSetting from "./components/admin-console/ResourceGroupAdminSetting";
// 🚨 TEST-ONLY: 운영 안정화 후 이 import 와 아래 등록 라인을 함께 제거하세요.
import TestDataAdminSetting from "./components/admin-console/TestDataAdminSetting";
import { id as PluginId } from "./manifest";
import { PluginRegistry } from "./types/mattermost-webapp";

interface PluginStore {
  dispatch: (action: unknown) => void;
}

const RhsIcon: React.FC = () => (
  <svg
    width="20"
    height="20"
    viewBox="0 0 24 24"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    style={{ verticalAlign: "middle" }}
  >
    <rect x="3" y="6" width="18" height="13" rx="2" stroke="currentColor" strokeWidth="2" />
    <path d="M3 8l9 6 9-6" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" />
    <circle cx="18" cy="6" r="3.5" fill="#F5C24A" stroke="currentColor" strokeWidth="1.2" />
  </svg>
);

const APP_BAR_ICON =
  "data:image/svg+xml;utf8," +
  encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none">
       <rect x="3" y="6" width="18" height="13" rx="2" stroke="white" stroke-width="2"/>
       <path d="M3 8l9 6 9-6" stroke="white" stroke-width="2" stroke-linejoin="round"/>
       <circle cx="18" cy="6" r="3.5" fill="#F5C24A" stroke="white" stroke-width="1.2"/>
     </svg>`
  );

async function isVisibleToCurrentUser(): Promise<boolean> {
  const ctl = new AbortController();
  const timeoutId = window.setTimeout(() => ctl.abort(), 5000);
  try {
    const resp = await fetch(`/plugins/${PluginId}/api/visibility`, {
      credentials: "include",
      headers: { "X-Requested-With": "XMLHttpRequest" },
      signal: ctl.signal,
    });
    if (!resp.ok) return false; // fail closed
    const data = (await resp.json()) as { visible?: boolean };
    return Boolean(data.visible);
  } catch (e) {
    // 네트워크/JSON/타임아웃 모두 fail closed.
    // 운영에서 갑작스레 모두에게 보이는 사고를 막기 위함.
    // eslint-disable-next-line no-console
    console.warn(`[${PluginId}] visibility check failed, defaulting to hidden`, e);
    return false;
  } finally {
    window.clearTimeout(timeoutId);
  }
}

export default class Plugin {
  public async initialize(
    registry: PluginRegistry,
    store: PluginStore
  ): Promise<void> {
    // System Console 의 업무영역 관리 — admin 만 접근하므로 가시성 체크 없이 등록
    if (typeof registry.registerAdminConsoleCustomSetting === "function") {
      try {
        registry.registerAdminConsoleCustomSetting(
          "ResourceGroupAdmin",
          ResourceGroupAdminSetting
        );
        // 🚨 TEST-ONLY: 운영 안정화 후 이 등록 블록을 제거하세요.
        registry.registerAdminConsoleCustomSetting(
          "TestDataAdmin",
          TestDataAdminSetting
        );
        // eslint-disable-next-line no-console
        console.log(`[${PluginId}] admin console custom setting registered`);
      } catch (e) {
        // eslint-disable-next-line no-console
        console.warn(`[${PluginId}] admin console registration failed`, e);
      }
    } else {
      // eslint-disable-next-line no-console
      console.warn(`[${PluginId}] registerAdminConsoleCustomSetting unavailable — 업무영역 관리 UI 가 안 보일 수 있음`);
    }

    // 일반 사용자 UI — 가시성 통과 시에만 등록
    const visible = await isVisibleToCurrentUser();
    if (!visible) {
      // eslint-disable-next-line no-console
      console.log(`[${PluginId}] hidden by VisibleToUsers config`);
      return;
    }

    const rhs = registry.registerRightHandSidebarComponent(
      () => <RhsPanel />,
      "AI Service Reporter"
    );

    const togglePanel = () => {
      if (rhs && rhs.toggleRHSPlugin) {
        store.dispatch(rhs.toggleRHSPlugin);
      }
    };

    if (typeof registry.registerChannelHeaderButtonAction === "function") {
      registry.registerChannelHeaderButtonAction(
        <RhsIcon />,
        togglePanel,
        "AI Service Reporter",
        "AI Service Reporter — 구독 설정"
      );
    }

    if (typeof registry.registerAppBarComponent === "function") {
      try {
        registry.registerAppBarComponent(
          APP_BAR_ICON,
          togglePanel,
          "AI Service Reporter — 구독 설정"
        );
      } catch (e) {
        // eslint-disable-next-line no-console
        console.warn(`[${PluginId}] apps bar registration failed`, e);
      }
    }

    // eslint-disable-next-line no-console
    console.log(`[${PluginId}] plugin initialized`);
  }
}

declare global {
  interface Window {
    registerPlugin: (id: string, plugin: Plugin) => void;
  }
}

window.registerPlugin(PluginId, new Plugin());
