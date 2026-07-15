// =============================================================================
// 구독 설정 패널 — 일반 사용자 UI
//
// 부모(RhsPanel)가 .asr-rhs 컨테이너를 제공하므로 본 컴포넌트는 fragment 로 출력.
// =============================================================================

import React, { useCallback, useEffect, useMemo, useState } from "react";
import { ApiClient } from "../../client";
import { RESERVED_SUMMARY_CODE } from "../../constants";
import {
  GetSubscriptionResponse,
  ResourceGroup,
} from "../../types/api";

type Phase = "loading" | "ready" | "saving" | "error";

const SubscriptionPanel: React.FC = () => {
  const [initial, setInitial] = useState<GetSubscriptionResponse | null>(null);
  const [active, setActive] = useState<boolean>(true);
  const [selectedGroups, setSelectedGroups] = useState<Set<string>>(new Set());
  const [channelID, setChannelID] = useState<string>("");
  const [search, setSearch] = useState<string>("");
  const [phase, setPhase] = useState<Phase>("loading");
  const [errorMsg, setErrorMsg] = useState<string>("");

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const data = await ApiClient.getSubscription();
        if (!alive) return;
        setInitial(data);
        if (data.subscribed && data.subscription) {
          setActive(data.subscription.active);
          setSelectedGroups(new Set(data.subscription.resource_groups));
          setChannelID(data.subscription.channel_id || "");
        } else {
          setActive(true);
          setSelectedGroups(new Set());
          setChannelID("");
        }
        setPhase("ready");
      } catch (e) {
        if (!alive) return;
        setErrorMsg(e instanceof Error ? e.message : "초기 로드 실패");
        setPhase("error");
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  const filteredGroups: ResourceGroup[] = useMemo(() => {
    // SUMMARY 는 일 단위 글로벌 Summary 전용 sentinel 코드.
    // 서버는 마스터에 시드하지 않지만, 누가 수동 삽입해도 사용자 칩에는 안 보이도록 방어.
    const all = (initial?.resource_groups || []).filter(
      (g) => g.code !== RESERVED_SUMMARY_CODE
    );
    if (!search.trim()) return all;
    const q = search.trim().toLowerCase();
    return all.filter(
      (g) =>
        g.code.toLowerCase().includes(q) ||
        g.name.toLowerCase().includes(q) ||
        (g.definition || "").toLowerCase().includes(q)
    );
  }, [initial?.resource_groups, search]);

  const toggleGroup = useCallback((code: string) => {
    setSelectedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(code)) next.delete(code);
      else next.add(code);
      return next;
    });
  }, []);

  const selectAllFiltered = useCallback(() => {
    setSelectedGroups((prev) => {
      const next = new Set(prev);
      filteredGroups.forEach((g) => next.add(g.code));
      return next;
    });
  }, [filteredGroups]);

  const clearAllFiltered = useCallback(() => {
    setSelectedGroups((prev) => {
      const next = new Set(prev);
      filteredGroups.forEach((g) => next.delete(g.code));
      return next;
    });
  }, [filteredGroups]);

  const handleSave = useCallback(async () => {
    setPhase("saving");
    setErrorMsg("");
    try {
      const resp = await ApiClient.putSubscription({
        active,
        channel_id: channelID || undefined,
        resource_groups: Array.from(selectedGroups).sort(),
      });
      setInitial(resp);
      setPhase("ready");
    } catch (e) {
      setErrorMsg(e instanceof Error ? e.message : "저장 실패");
      setPhase("error");
    }
  }, [active, channelID, selectedGroups]);

  const handleDelete = useCallback(async () => {
    if (!window.confirm("구독을 해제하시겠어요? 다음 발송부터 메시지가 오지 않습니다.")) {
      return;
    }
    setPhase("saving");
    setErrorMsg("");
    try {
      await ApiClient.deleteSubscription();
      setActive(false);
      setSelectedGroups(new Set());
      setChannelID("");
      const data = await ApiClient.getSubscription();
      setInitial(data);
      setPhase("ready");
    } catch (e) {
      setErrorMsg(e instanceof Error ? e.message : "구독 해제 실패");
      setPhase("error");
    }
  }, []);

  const handleReset = useCallback(() => {
    if (!initial) return;
    if (initial.subscribed && initial.subscription) {
      setActive(initial.subscription.active);
      setSelectedGroups(new Set(initial.subscription.resource_groups));
      setChannelID(initial.subscription.channel_id || "");
    } else {
      setActive(true);
      setSelectedGroups(new Set());
      setChannelID("");
    }
    setErrorMsg("");
    setPhase("ready");
  }, [initial]);

  if (phase === "loading") {
    return <div className="asr-status">불러오는 중…</div>;
  }

  const isSaving = phase === "saving";
  const groups = (initial?.resource_groups || []).filter((g) => g.code !== RESERVED_SUMMARY_CODE);
  const selectedCount = selectedGroups.size;
  const totalCount = groups.length;

  return (
    <div className="asr-subscription">
      {/* 헤더 */}
      <div className="asr-header">
        <h3>📬 구독 설정</h3>
        <p className="asr-sub">
          매일 아침 {initial?.delivery_time || "10:00"}, 선택한 업무영역의 분석 리포트를 받아보세요.
        </p>
      </div>

      {/* 에러 */}
      {errorMsg && (
        <div className="asr-error" role="alert">
          ⚠️ {errorMsg}
        </div>
      )}

      {/* 구독 토글 */}
      <div className="asr-section">
        <div className="asr-row">
          <label className="asr-label">
            <span>구독</span>
            <span className="asr-hint">{active ? "ON · 발송 받음" : "OFF · 발송 안 받음"}</span>
          </label>
          <button
            type="button"
            role="switch"
            aria-checked={active}
            className={`asr-toggle ${active ? "on" : "off"}`}
            onClick={() => setActive((v) => !v)}
            disabled={isSaving}
          >
            <span className="asr-toggle-thumb" />
          </button>
        </div>
      </div>

      {/* 리소스그룹 */}
      <div className={`asr-section ${active ? "" : "asr-disabled"}`}>
        <div className="asr-row">
          <label className="asr-label">
            <span>리소스그룹 (업무영역)</span>
            <span className="asr-hint">
              {selectedCount === 0
                ? `전체 ${totalCount}개 수신`
                : `${selectedCount} / ${totalCount} 선택`}
            </span>
          </label>
        </div>

        <input
          type="search"
          className="asr-search"
          placeholder="코드 또는 이름으로 검색"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          disabled={!active || isSaving}
        />

        <div className="asr-bulk">
          <button
            type="button"
            className="asr-link"
            onClick={selectAllFiltered}
            disabled={!active || isSaving}
          >
            현재 보이는 것 모두 선택
          </button>
          <span className="asr-sep">·</span>
          <button
            type="button"
            className="asr-link"
            onClick={clearAllFiltered}
            disabled={!active || isSaving}
          >
            현재 보이는 것 모두 해제
          </button>
        </div>

        <div className="asr-chip-grid">
          {filteredGroups.map((g) => {
            const on = selectedGroups.has(g.code);
            return (
              <button
                type="button"
                key={g.code}
                className={`asr-chip ${on ? "on" : ""}`}
                onClick={() => toggleGroup(g.code)}
                disabled={!active || isSaving}
                title={g.definition || g.name}
              >
                <span className="asr-chip-code">{g.code}</span>
                {on && <span className="asr-chip-check">✓</span>}
              </button>
            );
          })}
          {filteredGroups.length === 0 && (
            <div className="asr-empty">검색 결과가 없습니다.</div>
          )}
        </div>
      </div>

      {/* 발송 채널 */}
      <div className={`asr-section ${active ? "" : "asr-disabled"}`}>
        <div className="asr-row">
          <label className="asr-label">
            <span>발송 채널</span>
            <span className="asr-hint">{channelID ? "지정 채널" : "본인 DM"}</span>
          </label>
        </div>
        <input
          type="text"
          className="asr-text"
          placeholder="비워두면 봇 DM. 채널 ID를 입력하면 해당 채널로"
          value={channelID}
          onChange={(e) => setChannelID(e.target.value.trim())}
          disabled={!active || isSaving}
        />
        <p className="asr-hint">※ 채널 발송 시 봇이 해당 채널의 멤버여야 합니다.</p>
      </div>

      {/* 발송 시간 (read-only) */}
      <div className="asr-section asr-disabled">
        <div className="asr-row">
          <label className="asr-label">
            <span>발송 시간</span>
            <span className="asr-hint">시스템 콘솔 설정값</span>
          </label>
        </div>
        <input
          type="text"
          className="asr-text"
          value={initial?.delivery_time || "10:00"}
          readOnly
        />
      </div>

      {/* 푸터 */}
      <div className="asr-footer">
        <button
          type="button"
          className="asr-btn asr-btn-danger"
          onClick={handleDelete}
          disabled={isSaving || !initial?.subscribed}
        >
          구독 해제
        </button>
        <div className="asr-footer-right">
          <button
            type="button"
            className="asr-btn asr-btn-secondary"
            onClick={handleReset}
            disabled={isSaving}
          >
            취소
          </button>
          <button
            type="button"
            className="asr-btn asr-btn-primary"
            onClick={handleSave}
            disabled={isSaving}
          >
            {isSaving ? "저장 중…" : "저장"}
          </button>
        </div>
      </div>
    </div>
  );
};

export default SubscriptionPanel;
