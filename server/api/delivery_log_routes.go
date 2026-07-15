package api

import (
	"net/http"
	"strconv"
)

// handleListMyDeliveryLog — GET /api/delivery-log/me?limit=50
// 본인 발송 이력만 최근순으로 반환.
func (a *API) handleListMyDeliveryLog(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-Id")

	if a.deps.DeliveryLogStore == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB가 아직 준비되지 않았습니다.")
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	logs, err := a.deps.DeliveryLogStore.ListByUser(userID, limit)
	if err != nil {
		a.deps.PluginAPI.LogError("Failed to list delivery logs",
			"userID", maskUserID(userID), "error", err.Error())
		sendError(w, http.StatusInternalServerError, "LOG_LIST_FAILED", "발송 이력 조회 실패")
		return
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"items": logs,
	})
}
