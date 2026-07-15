// =============================================================================
// RHS 컨테이너
//
// 관리자 CRUD 는 System Console 의 plugin settings 페이지로 이동했으므로
// 여기서는 사용자 구독 설정만 렌더링합니다.
// =============================================================================

import React from "react";
import SubscriptionPanel from "./SubscriptionPanel";
import "./styles.css";

const RhsPanel: React.FC = () => {
  return (
    <div className="asr-rhs">
      <SubscriptionPanel />
    </div>
  );
};

export default RhsPanel;
