package api

import "net/http"

// handleListResourceGroups — GET /api/resourcegroups
// 활성 리소스그룹 마스터를 sortorder 순으로 반환.
func (a *API) handleListResourceGroups(w http.ResponseWriter, r *http.Request) {
	if a.deps.ResourceGroupStore == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB가 아직 준비되지 않았습니다.")
		return
	}

	groups, err := a.deps.ResourceGroupStore.ListActive()
	if err != nil {
		a.deps.PluginAPI.LogError("Failed to list resource groups", "error", err.Error())
		sendError(w, http.StatusInternalServerError, "RG_LIST_FAILED", "리소스그룹 조회 실패")
		return
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"items": groups,
	})
}
