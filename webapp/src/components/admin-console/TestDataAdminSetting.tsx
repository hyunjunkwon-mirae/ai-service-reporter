// =============================================================================
// 🚨 TEST-ONLY ADMIN COMPONENT — 운영 안정화 후 제거 예정
//
// System Console "테스트 데이터 도구" 섹션을 렌더링합니다.
//
// 버튼:
//   [테스트 데이터 추가]       → POST /admin/test-data/seed-all
//                                (리소스그룹 마스터만 idempotent 시드)
//   [전체 테이블 DELETE]       → POST /admin/test-data/wipe (확인 prompt 후)
//
// 운영 적용 시:
//   1) plugin.json 의 TestDataAdmin 항목 제거
//   2) index.tsx 의 등록 라인 제거
//   3) 이 파일과 server/api/testdata_routes.go 제거
// =============================================================================

import React, { useCallback, useState } from "react";
import { ApiClient } from "../../client";

type Phase = "idle" | "busy" | "done" | "error";
type BusyKind = "seed" | "wipe" | null;

const TestDataAdminSetting: React.FC = () => {
  const [phase, setPhase] = useState<Phase>("idle");
  const [busyKind, setBusyKind] = useState<BusyKind>(null);
  const [message, setMessage] = useState<string>("");
  const [errorMsg, setErrorMsg] = useState<string>("");

  const runAction = useCallback(
    async (kind: Exclude<BusyKind, null>, fn: () => Promise<string>) => {
      setPhase("busy");
      setBusyKind(kind);
      setMessage("");
      setErrorMsg("");
      try {
        const msg = await fn();
        setMessage(msg);
        setPhase("done");
      } catch (e) {
        setErrorMsg(e instanceof Error ? e.message : "요청 실패");
        setPhase("error");
      } finally {
        setBusyKind(null);
      }
    },
    []
  );

  const handleSeed = useCallback(
    () =>
      runAction("seed", async () => {
        const res = await ApiClient.adminSeedAll();
        return res.message;
      }),
    [runAction]
  );

  const handleWipe = useCallback(() => {
    const ok = window.confirm(
      "5개 테이블 전체 DELETE 합니다.\n" +
        "  · ai_service_reporter_delivery_log\n" +
        "  · ai_service_reporter_subscription_groups\n" +
        "  · ai_service_reporter_subscriptions\n" +
        "  · ai_service_reporter_reports\n" +
        "  · ai_service_reporter_resourcegroups\n\n" +
        "리소스그룹 마스터는 '테스트 데이터 추가' 버튼이나 플러그인 재시작으로 복구할 수 있습니다.\n" +
        "정말 진행하시겠습니까?"
    );
    if (!ok) return;
    void runAction("wipe", async () => {
      const res = await ApiClient.adminWipeAllTables();
      const lines = Object.entries(res.deleted)
        .map(([t, n]) => `  · ${t}: ${n}행`)
        .join("\n");
      return `${res.message}\n\n${lines}`;
    });
  }, [runAction]);

  const busy = phase === "busy";
  const buttonLabel = (kind: Exclude<BusyKind, null>, idle: string) =>
    busyKind === kind ? "처리 중…" : idle;

  return (
    <div
      style={{
        padding: "12px",
        border: "1px solid #f3c14c",
        borderRadius: 6,
        background: "#fff8e1",
      }}
    >
      <div style={{ fontSize: 13, marginBottom: 10, color: "#7a5a00" }}>
        ⚠️ 운영 적용 후 제거될 임시 도구입니다.
      </div>

      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        <button
          type="button"
          className="btn btn-primary"
          disabled={busy}
          onClick={handleSeed}
        >
          {buttonLabel("seed", "테스트 데이터 추가")}
        </button>
        <button
          type="button"
          className="btn btn-danger"
          disabled={busy}
          onClick={handleWipe}
        >
          {buttonLabel("wipe", "5개 테이블 전체 DELETE")}
        </button>
      </div>

      {message && (
        <pre
          style={{
            marginTop: 12,
            padding: 10,
            background: "#f6f8fa",
            border: "1px solid #d0d7de",
            borderRadius: 4,
            fontSize: 12,
            whiteSpace: "pre-wrap",
            color: "#1f2328",
          }}
        >
          ✅ {message}
        </pre>
      )}

      {errorMsg && (
        <div
          style={{
            marginTop: 12,
            padding: 10,
            background: "#ffeaea",
            border: "1px solid #f5a6a6",
            borderRadius: 4,
            fontSize: 12,
            color: "#a3261e",
          }}
        >
          ❌ {errorMsg}
        </div>
      )}

      <div style={{ marginTop: 12, fontSize: 12, color: "#666" }}>
        <div>
          <strong>테스트 데이터 추가</strong>: 리소스그룹 마스터만 idempotent 시드합니다. 이미 존재하는 행은 그대로 두고 누락된 코드만 추가.
        </div>
        <div>
          <strong>전체 DELETE</strong>: 5개 테이블 모두 비웁니다. 마스터도 함께 삭제되므로 시드 재실행 필요.
        </div>
      </div>
    </div>
  );
};

export default TestDataAdminSetting;
