package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/miraeasset/ai-service-reporter-plugin/server/bot"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

const PluginID = "ai_service_reporter"

type Dependencies struct {
	PluginAPI          plugin.API
	Client             *pluginapi.Client
	Bot                *bot.AIServiceReporterBot
	SubscriptionStore  *SubscriptionStore
	ResourceGroupStore *ResourceGroupStore // DB CRUD
	ReportStore        *ReportStore
	DeliveryLogStore   *DeliveryLogStore
	WebhookSecret      string

	// 🚨 TEST-ONLY: 운영 안정화 후 제거 예정 (testdata_routes.go 의 wipe 핸들러용)
	DB *sql.DB

	GetDeliveryTime func() string
	GetVisibleUsers func() string
	GetRenderConfig func() bot.RenderConfig
}

type API struct {
	router *mux.Router
	deps   Dependencies
}

func NewAPI(deps Dependencies) *API {
	a := &API{
		router: mux.NewRouter(),
		deps:   deps,
	}
	a.initRoutes()
	return a
}

func (a *API) GetRouter() *mux.Router {
	return a.router
}

func (a *API) initRoutes() {
	r := a.router.PathPrefix("/api").Subrouter()

	// ----- 구독 CRUD (사용자 인증) -----
	r.HandleFunc("/subscription", a.requireUser(a.handleGetSubscription)).Methods("GET")
	r.HandleFunc("/subscription", a.requireUser(a.handlePutSubscription)).Methods("PUT")
	r.HandleFunc("/subscription", a.requireUser(a.handleDeleteSubscription)).Methods("DELETE")
	r.HandleFunc("/subscription/delete", a.requireUser(a.handleDeleteSubscription)).Methods("POST")

	// ----- 마스터 조회 (사용자 인증, RHS UI 의 칩 그리드용) -----
	r.HandleFunc("/resourcegroups", a.requireUser(a.handleListResourceGroups)).Methods("GET")

	// ----- 발송 이력 (본인) -----
	r.HandleFunc("/delivery-log/me", a.requireUser(a.handleListMyDeliveryLog)).Methods("GET")

	// ----- 표출 권한 + admin 여부 (사용자 인증) -----
	r.HandleFunc("/visibility", a.requireUser(a.handleVisibility)).Methods("GET")

	// ----- 관리자 CRUD (system_admin 권한) -----
	r.HandleFunc("/admin/resourcegroups", a.requireSystemAdmin(a.handleAdminListResourceGroups)).Methods("GET")
	r.HandleFunc("/admin/resourcegroups", a.requireSystemAdmin(a.handleAdminCreateResourceGroup)).Methods("POST")
	r.HandleFunc("/admin/resourcegroups/{code}", a.requireSystemAdmin(a.handleAdminUpdateResourceGroup)).Methods("PUT")
	r.HandleFunc("/admin/resourcegroups/{code}", a.requireSystemAdmin(a.handleAdminDeleteResourceGroup)).Methods("DELETE")

	// ----- AIOps 가 호출하는 엔드포인트 (Secret 인증) -----
	r.HandleFunc("/aiops/resource-groups", a.handleAIOpsResourceGroups).Methods("GET")

	// ----- LLM Webhook (Secret 인증) -----
	r.HandleFunc("/webhook/llm-report", a.handleLLMReport).Methods("POST")
	r.HandleFunc("/webhook/llm-summary", a.handleLLMSummary).Methods("POST")

	// 🚨 TEST-ONLY: 운영 안정화 후 이 블록과 testdata_routes.go 를 함께 제거하세요.
	r.HandleFunc("/admin/test-data/seed-all", a.requireSystemAdmin(a.handleSeedAll)).Methods("POST")
	r.HandleFunc("/admin/test-data/wipe", a.requireSystemAdmin(a.handleWipeAllTables)).Methods("POST")
}

// =============================================================================
// 인증 미들웨어
// =============================================================================

// requireUser 는 Mattermost 세션 인증을 통과한 사용자만 허용.
func (a *API) requireUser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-Id")
		if userID == "" {
			sendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "로그인이 필요합니다.")
			return
		}
		h(w, r)
	}
}

// requireSystemAdmin 는 Mattermost system_admin 권한을 가진 사용자만 허용.
func (a *API) requireSystemAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-Id")
		if userID == "" {
			sendError(w, http.StatusUnauthorized, "UNAUTHORIZED", "로그인이 필요합니다.")
			return
		}
		if !a.deps.PluginAPI.HasPermissionTo(userID, model.PermissionManageSystem) {
			a.deps.PluginAPI.LogWarn("Admin endpoint access denied",
				"userID", maskUserID(userID), "path", r.URL.Path)
			sendError(w, http.StatusForbidden, "FORBIDDEN", "관리자 권한이 필요합니다.")
			return
		}
		h(w, r)
	}
}

func (a *API) currentDeliveryTime() string {
	if a.deps.GetDeliveryTime != nil {
		if t := a.deps.GetDeliveryTime(); t != "" {
			return t
		}
	}
	return "10:00"
}

// =============================================================================
// 표출 권한 + admin 여부 (GET /api/visibility)
//
// 응답:
//   { "visible": true | false, "isAdmin": true | false }
// =============================================================================

// handleVisibility 는 사이드바/RHS/Apps Bar 표출 여부를 판단합니다.
//
// 규칙:
//   - VisibleToUsers 가 빈 값 또는 "*" : 모두 표출
//   - 그 외 : 콤마구분 username 목록과 정확 일치하는 사용자만 표출
//   - admin 권한과 무관 — admin 도 username 매칭이 안 되면 안 보임
//     (관리자 권한이 여러 명에게 분산돼 있어 admin 자동 통과는 위험)
//
// 입력칸에 입력하는 값은 사번(=username)이며, 본 함수는 user.Username 과만 비교.
// email/nickname 등 다른 식별자와는 비교하지 않음.
func (a *API) handleVisibility(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-Id")
	isAdmin := a.deps.PluginAPI.HasPermissionTo(userID, model.PermissionManageSystem)

	list := ""
	if a.deps.GetVisibleUsers != nil {
		list = strings.TrimSpace(a.deps.GetVisibleUsers())
	}

	// 빈 값 또는 * → 모두 표출 (개발 단계 편의용. 운영에선 명시 목록 권장)
	if list == "" || list == "*" {
		sendJSON(w, http.StatusOK, map[string]bool{
			"visible": true,
			"isAdmin": isAdmin,
		})
		return
	}

	user, appErr := a.deps.PluginAPI.GetUser(userID)
	if appErr != nil || user == nil {
		sendJSON(w, http.StatusOK, map[string]bool{
			"visible": false,
			"isAdmin": isAdmin,
		})
		return
	}

	// username 기준 정확 일치만 인정. admin 자동 통과 없음.
	visible := false
	for _, p := range strings.Split(list, ",") {
		if strings.TrimSpace(p) == user.Username {
			visible = true
			break
		}
	}

	sendJSON(w, http.StatusOK, map[string]bool{
		"visible": visible,
		"isAdmin": isAdmin,
	})
}
