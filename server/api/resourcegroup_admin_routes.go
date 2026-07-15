package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

// =============================================================================
// 관리자 리소스그룹 CRUD API
//
// 인증: requireSystemAdmin (Mattermost system_admin 권한 필요)
//
// 엔드포인트:
//   GET    /api/admin/resourcegroups       — 전체 (비활성 포함) 목록
//   POST   /api/admin/resourcegroups       — 신규 생성
//   PUT    /api/admin/resourcegroups/{code}— 수정
//   DELETE /api/admin/resourcegroups/{code}— 영구 삭제
// =============================================================================

// AdminResourceGroupRequest 는 POST/PUT 요청 본문입니다.
type AdminResourceGroupRequest struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Definition *string `json:"definition,omitempty"`
	SortOrder   int     `json:"sort_order"`
	Active      bool    `json:"active"`
}

// GET /api/admin/resourcegroups
func (a *API) handleAdminListResourceGroups(w http.ResponseWriter, r *http.Request) {
	if a.deps.ResourceGroupStore == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB 가 아직 준비되지 않았습니다.")
		return
	}
	groups, err := a.deps.ResourceGroupStore.ListAll()
	if err != nil {
		a.deps.PluginAPI.LogError("Failed to list all resource groups", "error", err.Error())
		sendError(w, http.StatusInternalServerError, "DB_QUERY_FAILED", "DB 조회 실패")
		return
	}
	if groups == nil {
		groups = []*ResourceGroup{}
	}
	sendJSON(w, http.StatusOK, map[string]interface{}{"items": groups})
}

// POST /api/admin/resourcegroups
func (a *API) handleAdminCreateResourceGroup(w http.ResponseWriter, r *http.Request) {
	if a.deps.ResourceGroupStore == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB 가 아직 준비되지 않았습니다.")
		return
	}

	var req AdminResourceGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "INVALID_BODY", "요청 본문 파싱 실패")
		return
	}
	defer r.Body.Close()

	req.Code = strings.TrimSpace(strings.ToUpper(req.Code))
	if req.Code == "" {
		sendError(w, http.StatusBadRequest, "MISSING_CODE", "code 는 필수입니다.")
		return
	}

	rg := &ResourceGroup{
		Code:        req.Code,
		Name:        req.Name,
		Definition: req.Definition,
		SortOrder:   req.SortOrder,
		Active:      req.Active,
	}
	if rg.Name == "" {
		rg.Name = rg.Code
	}

	if err := a.deps.ResourceGroupStore.Create(rg); err != nil {
		a.deps.PluginAPI.LogError("Failed to create resource group",
			"code", rg.Code, "error", err.Error())
		sendError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
		return
	}

	a.deps.PluginAPI.LogInfo("Resource group created", "code", rg.Code)
	sendJSON(w, http.StatusCreated, rg)
}

// PUT /api/admin/resourcegroups/{code}
func (a *API) handleAdminUpdateResourceGroup(w http.ResponseWriter, r *http.Request) {
	if a.deps.ResourceGroupStore == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB 가 아직 준비되지 않았습니다.")
		return
	}

	vars := mux.Vars(r)
	code := strings.TrimSpace(strings.ToUpper(vars["code"]))
	if code == "" {
		sendError(w, http.StatusBadRequest, "MISSING_CODE", "URL 의 code 가 필요합니다.")
		return
	}

	var req AdminResourceGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "INVALID_BODY", "요청 본문 파싱 실패")
		return
	}
	defer r.Body.Close()

	rg := &ResourceGroup{
		Code:        code,
		Name:        req.Name,
		Definition: req.Definition,
		SortOrder:   req.SortOrder,
		Active:      req.Active,
	}
	if rg.Name == "" {
		rg.Name = rg.Code
	}

	if err := a.deps.ResourceGroupStore.Update(rg); err != nil {
		a.deps.PluginAPI.LogError("Failed to update resource group",
			"code", code, "error", err.Error())
		sendError(w, http.StatusBadRequest, "UPDATE_FAILED", err.Error())
		return
	}

	a.deps.PluginAPI.LogInfo("Resource group updated", "code", code)
	sendJSON(w, http.StatusOK, rg)
}

// DELETE /api/admin/resourcegroups/{code}
func (a *API) handleAdminDeleteResourceGroup(w http.ResponseWriter, r *http.Request) {
	if a.deps.ResourceGroupStore == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB 가 아직 준비되지 않았습니다.")
		return
	}

	vars := mux.Vars(r)
	code := strings.TrimSpace(strings.ToUpper(vars["code"]))
	if code == "" {
		sendError(w, http.StatusBadRequest, "MISSING_CODE", "URL 의 code 가 필요합니다.")
		return
	}

	if err := a.deps.ResourceGroupStore.Delete(code); err != nil {
		a.deps.PluginAPI.LogError("Failed to delete resource group",
			"code", code, "error", err.Error())
		sendError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}

	a.deps.PluginAPI.LogInfo("Resource group deleted", "code", code)
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"code":    code,
	})
}
