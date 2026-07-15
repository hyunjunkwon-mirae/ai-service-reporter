package api

import "net/http"

// =============================================================================
// AIOps Routes
//
// AIOps 서버가 우리 플러그인에서 데이터를 가져갈 때 호출하는 엔드포인트들.
//
// 인증: X-AI-Service-Reporter-Secret 헤더 = 플러그인 설정 LLMWebhookSecret
//       (LLM Webhook 과 동일 시크릿 사용)
//
// 데이터 소스: ai_service_reporter_resourcegroups DB 테이블 (active=true)
// =============================================================================

// GET /api/aiops/resource-groups
// 응답: ["AC","AD","AG", ...]  — active=true 인 코드만, sortorder 순
func (a *API) handleAIOpsResourceGroups(w http.ResponseWriter, r *http.Request) {
	if !a.verifyWebhookSecret(r) {
		a.deps.PluginAPI.LogWarn("AIOps API unauthorized",
			"path", r.URL.Path, "remoteAddr", r.RemoteAddr)
		sendError(w, http.StatusUnauthorized, "INVALID_SECRET",
			"X-AI-Service-Reporter-Secret 헤더가 유효하지 않습니다.")
		return
	}

	if a.deps.ResourceGroupStore == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY",
			"DB 가 아직 준비되지 않았습니다.")
		return
	}

	codes, err := a.deps.ResourceGroupStore.ListActiveCodes()
	if err != nil {
		a.deps.PluginAPI.LogError("Failed to fetch resource groups from DB", "error", err.Error())
		sendError(w, http.StatusInternalServerError, "DB_QUERY_FAILED",
			"DB 조회 실패")
		return
	}

	if codes == nil {
		codes = []string{}
	}
	sendJSON(w, http.StatusOK, codes)
}
