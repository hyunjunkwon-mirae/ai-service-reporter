// =============================================================================
// 시스템 콘솔용 업무영역 관리 컴포넌트
//
// 위치: System Console → Plugins → AI Service Reporter → "업무영역 코드 관리"
//
// 동작:
//   · 자체적으로 GET /api/admin/resourcegroups 로 목록 로드
//   · 추가/수정/삭제는 POST/PUT/DELETE /api/admin/resourcegroups[/code]
//   · System Console 의 표준 Save 흐름을 사용하지 않음 (onChange 안 부름)
//     → CRUD 시점에 즉시 DB 반영, "저장 안함" 경고 안 뜸
// =============================================================================

import React, { useCallback, useEffect, useRef, useState } from "react";
import { ApiClient } from "../../client";
import { RESERVED_SUMMARY_CODE } from "../../constants";
import { ResourceGroup } from "../../types/api";
import "./styles.css";

type Phase = "loading" | "ready" | "saving" | "error";

interface FormState {
  code: string;
  name: string;
  definition: string;
  sort_order: number;
  active: boolean;
  editingCode: string | null;
}

const emptyForm = (): FormState => ({
  code: "",
  name: "",
  definition: "",
  sort_order: 10,
  active: true,
  editingCode: null,
});

// Mattermost 가 custom setting 컴포넌트에 넘기는 props.
// 이 컴포넌트는 표준 Save flow 를 쓰지 않고 즉시 DB 반영하는 자체 CRUD 라
// `disabled` prop 은 따르지 않습니다 (Mattermost 가 다른 plugin 설정 검증 도중
// disabled=true 로 묶어버려서 추가/수정 칸이 비활성화되는 케이스가 있음).
interface SettingProps {
  id?: string;
  label?: React.ReactNode;
  helpText?: React.ReactNode;
  disabled?: boolean;
}

const ResourceGroupAdminSetting: React.FC<SettingProps> = () => {
  const [items, setItems] = useState<ResourceGroup[]>([]);
  const [form, setForm] = useState<FormState>(emptyForm());
  // 초기값을 "ready" 로 둠. "loading" 으로 시작하면 첫 마운트~ admin list 응답 사이에
  // formDisabled 가 true 가 되어 사용자가 펼치자마자 입력칸이 비활성화되는 버그가 생김.
  // load() 안에서 setPhase("loading") 을 따로 호출하지만, formDisabled 는 "saving" 만 본다.
  const [phase, setPhase] = useState<Phase>("ready");
  const [errorMsg, setErrorMsg] = useState<string>("");
  // 자주 안 만지는 영역이라 기본 접힘. 사용자가 펼칠 때만 폼/테이블 표시.
  const [expanded, setExpanded] = useState<boolean>(false);

  // Mattermost System Console 이 우리 컴포넌트를 <fieldset disabled> 안에 넣는
  // 경우가 있어서, 그러면 컴포넌트 내부의 모든 input/button 이 HTML 단에서 잠긴다.
  // (CSS / React prop 으로는 풀 수 없음 — HTML 속성이라 JS 로 제거해야 함.)
  // 마운트 후 조상 트리를 거슬러 올라가서 disabled 속성을 강제 제거하고,
  // MutationObserver 로 다시 setAttribute("disabled") 되는 것도 즉시 해제한다.
  const rootRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;

    const ancestors: HTMLFieldSetElement[] = [];
    let p = root.parentElement;
    while (p) {
      if (p.tagName === "FIELDSET") {
        ancestors.push(p as HTMLFieldSetElement);
      }
      p = p.parentElement;
    }
    if (ancestors.length === 0) return;

    const unlock = () => {
      ancestors.forEach((fs) => {
        if (fs.hasAttribute("disabled")) {
          fs.removeAttribute("disabled");
        }
      });
    };

    unlock();

    const observers = ancestors.map((fs) => {
      const obs = new MutationObserver(() => {
        if (fs.hasAttribute("disabled")) {
          fs.removeAttribute("disabled");
        }
      });
      obs.observe(fs, { attributes: true, attributeFilter: ["disabled"] });
      return obs;
    });

    return () => {
      observers.forEach((o) => o.disconnect());
    };
  }, []);

  const load = useCallback(async () => {
    setPhase("loading");
    setErrorMsg("");
    try {
      const resp = await ApiClient.adminListResourceGroups();
      // SUMMARY 는 일 단위 글로벌 Summary 전용 sentinel 코드.
      // CRUD 대상이 아니므로 목록에서 숨김.
      const filtered = (resp.items || []).filter((x) => x.code !== RESERVED_SUMMARY_CODE);
      setItems(filtered);
      setPhase("ready");
    } catch (e) {
      setErrorMsg(e instanceof Error ? e.message : "조회 실패");
      setPhase("error");
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const startCreate = useCallback(() => {
    const maxOrder = items.reduce((acc, x) => Math.max(acc, x.sort_order || 0), 0);
    setForm({ ...emptyForm(), sort_order: maxOrder + 10 });
    setErrorMsg("");
  }, [items]);

  const startEdit = useCallback((rg: ResourceGroup) => {
    setForm({
      code: rg.code,
      name: rg.name,
      definition: (rg as any).definition || "",
      sort_order: rg.sort_order,
      active: rg.active,
      editingCode: rg.code,
    });
    setErrorMsg("");
  }, []);

  const handleSave = useCallback(async () => {
    if (!form.code.trim()) {
      setErrorMsg("코드는 필수입니다.");
      return;
    }
    if (form.code.trim().toUpperCase() === RESERVED_SUMMARY_CODE) {
      setErrorMsg(`${RESERVED_SUMMARY_CODE} 는 예약된 코드입니다.`);
      return;
    }
    setPhase("saving");
    setErrorMsg("");
    try {
      const body = {
        code: form.code.trim().toUpperCase(),
        name: form.name.trim() || form.code.trim().toUpperCase(),
        definition: form.definition.trim() || undefined,
        sort_order: form.sort_order,
        active: form.active,
      };
      if (form.editingCode) {
        await ApiClient.adminUpdateResourceGroup(form.editingCode, body);
      } else {
        await ApiClient.adminCreateResourceGroup(body);
      }
      setForm(emptyForm());
      await load();
    } catch (e) {
      setErrorMsg(e instanceof Error ? e.message : "저장 실패");
      setPhase("error");
    }
  }, [form, load]);

  const handleDelete = useCallback(
    async (code: string) => {
      if (!window.confirm(`코드 "${code}" 를 영구 삭제하시겠어요?\n(외래키가 있다면 DB 정책에 따라 거부될 수 있음)`)) {
        return;
      }
      setPhase("saving");
      setErrorMsg("");
      try {
        await ApiClient.adminDeleteResourceGroup(code);
        if (form.editingCode === code) {
          setForm(emptyForm());
        }
        await load();
      } catch (e) {
        setErrorMsg(e instanceof Error ? e.message : "삭제 실패");
        setPhase("error");
      }
    },
    [form, load]
  );

  // 로딩 표시는 별도 분기로 분리하지 않는다. 조기 return 을 하면 본문의
  // rootRef 가 한 번도 본문 div 에 마운트되지 않은 채로 useEffect 가 돌아서
  // fieldset disabled 우회 로직이 엉뚱한 DOM 기준으로 동작한다.
  // (특히 items.length === 0 으로 시작하는 빈 DB 상태에서 입력칸 비활성화 이슈)
  // 본문은 항상 렌더링하고, 로딩 텍스트는 헤더 옆에 작게 표시만 한다.

  const isSaving = phase === "saving";
  // 입력칸/버튼은 실제 저장 진행 중일 때만 막는다.
  // loading 은 마운트 직후 admin list 응답을 기다리는 잠깐의 상태인데,
  // 그 사이 입력 차단까지 할 필요는 없음 (저장 시 어차피 동기 검증함).
  const formDisabled = isSaving;
  const isLoading = phase === "loading";

  // 요약 (접힘/펼침 공통)
  const activeCount = items.filter((x) => x.active).length;
  const summary = `총 ${items.length}개 (활성 ${activeCount}개)`;

  return (
    <div className="asr-sc-root" ref={rootRef}>
      <div className="asr-admin-header">
        <button
          type="button"
          className="asr-collapse-btn"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
          aria-label={expanded ? "접기" : "펼치기"}
        >
          <span className={`asr-chevron ${expanded ? "open" : ""}`}>▶</span>
          <strong>업무영역 코드</strong>
          <span className="asr-summary">{summary}</span>
        </button>
        {expanded && (
          <>
            {isLoading && (
              <span className="asr-status" style={{ marginRight: 8, fontSize: 12, color: "#666" }}>
                불러오는 중…
              </span>
            )}
            <button
              type="button"
              className="asr-btn asr-btn-secondary asr-btn-sm"
              onClick={load}
              disabled={isSaving || isLoading}
            >
              새로고침
            </button>
          </>
        )}
      </div>

      {errorMsg && (
        <div className="asr-error" role="alert">
          ⚠️ {errorMsg}
        </div>
      )}

      {!expanded && (
        <div className="asr-collapsed-hint">
          위 헤더를 클릭하면 코드 목록 / 추가 / 수정 / 삭제 UI 가 열립니다. (자주 만질 일이 없어서 기본 접혀있음)
        </div>
      )}

      {expanded && <>
      {/* 폼 */}
      <div className="asr-form">
        <h5>{form.editingCode ? `수정: ${form.editingCode}` : "새 코드 추가"}</h5>

        <div className="asr-form-grid">
          <label className="asr-field">
            <span>코드 *</span>
            <input
              type="text"
              className="asr-text"
              value={form.code}
              onChange={(e) => setForm((f) => ({ ...f, code: e.target.value }))}
              disabled={!!form.editingCode || formDisabled}
              placeholder="예: AC"
              maxLength={8}
            />
          </label>
          <label className="asr-field">
            <span>표시명</span>
            <input
              type="text"
              className="asr-text"
              value={form.name}
              onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
              disabled={formDisabled}
              placeholder="비워두면 코드와 동일"
            />
          </label>
          <label className="asr-field">
            <span>정렬 순서</span>
            <input
              type="number"
              className="asr-text"
              value={form.sort_order}
              onChange={(e) =>
                setForm((f) => ({ ...f, sort_order: parseInt(e.target.value, 10) || 0 }))
              }
              disabled={formDisabled}
              step={10}
            />
          </label>
          <label className="asr-field-row">
            <input
              type="checkbox"
              checked={form.active}
              onChange={(e) => setForm((f) => ({ ...f, active: e.target.checked }))}
              disabled={formDisabled}
            />
            <span>활성</span>
          </label>
        </div>

        <label className="asr-field">
          <span>설명</span>
          <input
            type="text"
            className="asr-text"
            value={form.definition}
            onChange={(e) => setForm((f) => ({ ...f, definition: e.target.value }))}
            disabled={formDisabled}
            placeholder="선택 (DB definition 컬럼)"
          />
        </label>

        <div className="asr-form-buttons">
          <button
            type="button"
            className="asr-btn asr-btn-secondary"
            onClick={() => setForm(emptyForm())}
            disabled={formDisabled}
          >
            초기화
          </button>
          <button
            type="button"
            className="asr-btn asr-btn-primary"
            onClick={handleSave}
            disabled={formDisabled}
          >
            {isSaving ? "저장 중…" : form.editingCode ? "수정 저장" : "추가"}
          </button>
        </div>
      </div>

      {/* 목록 */}
      <div className="asr-list">
        <div className="asr-list-header">
          <strong>현재 코드 목록</strong>
          <button
            type="button"
            className="asr-link"
            onClick={startCreate}
            disabled={formDisabled}
          >
            + 새 코드 추가
          </button>
        </div>
        {items.length === 0 && (
          <div className="asr-empty">등록된 코드가 없습니다. 위에서 추가하세요.</div>
        )}
        {items.length > 0 && (
          <table className="asr-table">
            <thead>
              <tr>
                <th>코드</th>
                <th>표시명</th>
                <th>설명</th>
                <th>순서</th>
                <th>활성</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {items.map((rg) => (
                <tr key={rg.code} className={rg.active ? "" : "asr-row-inactive"}>
                  <td><strong>{rg.code}</strong></td>
                  <td>{rg.name}</td>
                  <td>{(rg as any).definition || "—"}</td>
                  <td>{rg.sort_order}</td>
                  <td>{rg.active ? "✓" : "✗"}</td>
                  <td className="asr-actions">
                    <button
                      type="button"
                      className="asr-link"
                      onClick={() => startEdit(rg)}
                      disabled={formDisabled}
                    >
                      수정
                    </button>
                    <span className="asr-sep">·</span>
                    <button
                      type="button"
                      className="asr-link asr-link-danger"
                      onClick={() => handleDelete(rg.code)}
                      disabled={formDisabled}
                    >
                      삭제
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      </>}
    </div>
  );
};

export default ResourceGroupAdminSetting;
