package api

// =============================================================================
// 🚨 TEST-ONLY ROUTES — 운영 안정화 후 제거 예정
//
// System Console "테스트 데이터 도구" 섹션에서 호출되는 일회성 시드/삭제 핸들러.
//
//   POST /admin/test-data/seed-all   → 리소스그룹 마스터 시드 (bootstrap 재사용, idempotent)
//   POST /admin/test-data/wipe       → 5개 테이블 전체 DELETE
//
// 운영 적용 시 이 파일과 api.go 의 라우터 등록 라인 / webapp 의
// TestDataAdminSetting 컴포넌트를 모두 삭제하면 됩니다.
//
// 리포트 가데이터 시드 기능은 AIOps 가 실제 데이터를 적재하는 환경으로
// 전환되면서 제거되었습니다.
// =============================================================================

import (
	"fmt"
	"net/http"
)

// -----------------------------------------------------------------------------
// POST /admin/test-data/seed-all
//
// 리소스그룹 마스터(ai_service_reporter_resourcegroups) 만 시드합니다.
// bootstrap 로직을 재호출하므로 이미 존재하는 행은 건드리지 않음 (ON CONFLICT DO NOTHING).
// -----------------------------------------------------------------------------

func (a *API) handleSeedAll(w http.ResponseWriter, r *http.Request) {
	if a.deps.DB == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB가 아직 준비되지 않았습니다.")
		return
	}

	TryBootstrapResourceGroups(a.deps.DB, a.deps.PluginAPI)

	rgCount := 0
	if a.deps.ResourceGroupStore != nil {
		all, _ := a.deps.ResourceGroupStore.ListAll()
		rgCount = len(all)
	}

	a.deps.PluginAPI.LogInfo("Test data seed-all completed",
		"rg_count", rgCount)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"resource_group_count": rgCount,
		"message":              fmt.Sprintf("리소스그룹 마스터 %d건 확인 완료. (이미 존재하는 행은 그대로 유지)", rgCount),
	})
}

// -----------------------------------------------------------------------------
// POST /admin/test-data/wipe
//
// 5개 테이블 전체 DELETE. 마스터(resourcegroups)도 함께 삭제되므로 wipe 후에는
// "테스트 데이터 추가" 버튼을 다시 눌러 마스터를 복구하거나, 플러그인을 재시작해서
// bootstrap 으로 복구하세요.
// -----------------------------------------------------------------------------

func (a *API) handleWipeAllTables(w http.ResponseWriter, r *http.Request) {
	if a.deps.DB == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB가 아직 준비되지 않았습니다.")
		return
	}

	tables := []string{
		"ai_service_reporter_delivery_log",
		"ai_service_reporter_subscription_groups",
		"ai_service_reporter_subscriptions",
		"ai_service_reporter_reports",
		"ai_service_reporter_resourcegroups",
	}

	results := make(map[string]int64, len(tables))
	for _, t := range tables {
		res, err := a.deps.DB.Exec("DELETE FROM " + t)
		if err != nil {
			a.deps.PluginAPI.LogError("Wipe failed", "table", t, "error", err.Error())
			sendError(w, http.StatusInternalServerError, "WIPE_FAILED",
				fmt.Sprintf("%s DELETE 실패: %s", t, err.Error()))
			return
		}
		n, _ := res.RowsAffected()
		results[t] = n
	}

	a.deps.PluginAPI.LogWarn("All tables wiped via admin button", "results", results)

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"deleted": results,
		"message": "5개 테이블 모두 DELETE 완료. '테스트 데이터 추가' 버튼으로 복구하거나 플러그인을 재시작하세요.",
	})
}
