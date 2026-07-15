package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/miraeasset/ai-service-reporter-plugin/server/bot"
)

// =============================================================================
// LLM Webhook
//
// 두 엔드포인트:
//   1) POST /webhook/llm-report   — 업무영역별 anomaly 리포트
//   2) POST /webhook/llm-summary  — 일 단위 글로벌 Summary
//
// 인증:  X-AI-Service-Reporter-Secret 헤더 = 플러그인 설정 LLMWebhookSecret
//
// Summary 는 ai_service_reporter_reports 테이블에 resourcegroupcode='SUMMARY'
// 행으로 저장됩니다 (별도 테이블 없이 약속된 sentinel 코드 사용). 마스터 테이블에는
// 시드하지 않으므로 사용자 RHS 칩 그리드 / admin CRUD 에는 노출되지 않습니다.
// =============================================================================

const SummaryResourceGroupCode = "SUMMARY"

// -----------------------------------------------------------------------------
// 1) 업무영역별 anomaly 리포트
// -----------------------------------------------------------------------------

type LLMReportRequest struct {
	ReportDate        string        `json:"report_date"`
	ResourceGroupCode string        `json:"resource_group"`
	WindowDays        int           `json:"window_days"`
	Anomalies         []bot.Anomaly `json:"anomalies"`
}

func (a *API) handleLLMReport(w http.ResponseWriter, r *http.Request) {
	if !a.verifyWebhookSecret(r) {
		a.deps.PluginAPI.LogWarn("LLM webhook unauthorized", "remoteAddr", r.RemoteAddr)
		sendError(w, http.StatusUnauthorized, "INVALID_SECRET",
			"X-AI-Service-Reporter-Secret 헤더가 유효하지 않습니다.")
		return
	}
	if a.deps.ReportStore == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB가 아직 준비되지 않았습니다.")
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		sendError(w, http.StatusBadRequest, "READ_FAILED", "요청 본문 읽기 실패")
		return
	}
	defer r.Body.Close()

	var req LLMReportRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		sendError(w, http.StatusBadRequest, "INVALID_BODY", "요청 본문 파싱 실패")
		return
	}

	// 검증
	if req.ReportDate == "" {
		sendError(w, http.StatusBadRequest, "MISSING_REPORT_DATE", "report_date 가 필요합니다.")
		return
	}
	if _, perr := time.Parse("2006-01-02", req.ReportDate); perr != nil {
		sendError(w, http.StatusBadRequest, "INVALID_REPORT_DATE", "report_date 는 YYYY-MM-DD 형식이어야 합니다.")
		return
	}
	code := strings.TrimSpace(req.ResourceGroupCode)
	if code == "" {
		sendError(w, http.StatusBadRequest, "MISSING_RESOURCE_GROUP", "resource_group 이 필요합니다.")
		return
	}
	// SUMMARY 는 별도 엔드포인트 사용
	if code == SummaryResourceGroupCode {
		sendError(w, http.StatusBadRequest, "RESERVED_RESOURCE_GROUP",
			"resource_group 으로 SUMMARY 를 사용할 수 없습니다. /webhook/llm-summary 를 사용하세요.")
		return
	}
	if len(req.Anomalies) == 0 {
		sendError(w, http.StatusBadRequest, "MISSING_ANOMALIES", "anomalies 가 1건 이상 필요합니다.")
		return
	}
	if req.WindowDays <= 0 {
		req.WindowDays = 7
	}

	// 리소스그룹 코드 검증 — 마스터에 존재하는 활성 코드만 허용
	if a.deps.ResourceGroupStore != nil {
		validCodes, vErr := a.deps.ResourceGroupStore.AllValidCodes()
		if vErr == nil {
			if _, ok := validCodes[code]; !ok {
				sendError(w, http.StatusBadRequest, "UNKNOWN_RESOURCE_GROUP",
					"resource_group 코드가 마스터에 없습니다: "+code)
				return
			}
		}
	}

	// content 컬럼은 더 이상 표시용으로 쓰지 않음 (발송 시점에 rawdata 를 파싱).
	// 운영 디버깅 편의를 위해 anomaly 개수를 메모로 남김.
	memo := summarizeAnomaliesMemo(req.Anomalies)

	report := &Report{
		ReportDate:        req.ReportDate,
		ResourceGroupCode: code,
		WindowDays:        req.WindowDays,
		Content:           memo,
		RawData:           rawBody,
	}
	if err := a.deps.ReportStore.Upsert(report); err != nil {
		a.deps.PluginAPI.LogError("Failed to upsert LLM report",
			"date", req.ReportDate, "group", code, "error", err.Error())
		sendError(w, http.StatusInternalServerError, "REPORT_SAVE_FAILED", "리포트 저장 실패")
		return
	}

	a.deps.PluginAPI.LogInfo("LLM report received",
		"date", req.ReportDate,
		"group", code,
		"anomalies", len(req.Anomalies),
		"reportID", report.ID,
	)

	sendJSON(w, http.StatusAccepted, map[string]interface{}{
		"report_id": report.ID,
	})
}

// summarizeAnomaliesMemo 는 운영 디버깅용 짧은 메모를 생성합니다.
// 발송 메시지 본문이 아니라 reports.content 컬럼의 추적 보조 텍스트로 사용.
func summarizeAnomaliesMemo(anomalies []bot.Anomaly) string {
	nCall, nPct, nLat := 0, 0, 0
	for _, a := range anomalies {
		if a.CallCountRank != nil && *a.CallCountRank > 0 {
			nCall++
		}
		if a.CallCountPctRank != nil && *a.CallCountPctRank > 0 {
			nPct++
		}
		if a.LatencyDiffRank != nil && *a.LatencyDiffRank > 0 {
			nLat++
		}
	}
	return jsonMemo(map[string]int{
		"anomalies":           len(anomalies),
		"with_call_rank":      nCall,
		"with_call_pct_rank":  nPct,
		"with_latency_rank":   nLat,
	})
}

func jsonMemo(m map[string]int) string {
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// -----------------------------------------------------------------------------
// 2) 일 단위 글로벌 Summary
// -----------------------------------------------------------------------------

type LLMSummaryRequest struct {
	ReportDate string `json:"report_date"`
	Summary    string `json:"summary"`
}

func (a *API) handleLLMSummary(w http.ResponseWriter, r *http.Request) {
	if !a.verifyWebhookSecret(r) {
		a.deps.PluginAPI.LogWarn("LLM summary webhook unauthorized", "remoteAddr", r.RemoteAddr)
		sendError(w, http.StatusUnauthorized, "INVALID_SECRET",
			"X-AI-Service-Reporter-Secret 헤더가 유효하지 않습니다.")
		return
	}
	if a.deps.ReportStore == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB가 아직 준비되지 않았습니다.")
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		sendError(w, http.StatusBadRequest, "READ_FAILED", "요청 본문 읽기 실패")
		return
	}
	defer r.Body.Close()

	var req LLMSummaryRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		sendError(w, http.StatusBadRequest, "INVALID_BODY", "요청 본문 파싱 실패")
		return
	}

	if req.ReportDate == "" {
		sendError(w, http.StatusBadRequest, "MISSING_REPORT_DATE", "report_date 가 필요합니다.")
		return
	}
	if _, perr := time.Parse("2006-01-02", req.ReportDate); perr != nil {
		sendError(w, http.StatusBadRequest, "INVALID_REPORT_DATE", "report_date 는 YYYY-MM-DD 형식이어야 합니다.")
		return
	}
	if strings.TrimSpace(req.Summary) == "" {
		sendError(w, http.StatusBadRequest, "MISSING_SUMMARY", "summary 가 필요합니다.")
		return
	}

	report := &Report{
		ReportDate:        req.ReportDate,
		ResourceGroupCode: SummaryResourceGroupCode,
		WindowDays:        1, // Summary 는 일 단위 — 의미 없는 값. NOT NULL 컬럼이라 채워둠.
		Content:           strings.TrimSpace(req.Summary),
		RawData:           rawBody,
	}
	if err := a.deps.ReportStore.Upsert(report); err != nil {
		a.deps.PluginAPI.LogError("Failed to upsert LLM summary",
			"date", req.ReportDate, "error", err.Error())
		sendError(w, http.StatusInternalServerError, "REPORT_SAVE_FAILED", "Summary 저장 실패")
		return
	}

	a.deps.PluginAPI.LogInfo("LLM summary received",
		"date", req.ReportDate,
		"length", len(req.Summary),
		"reportID", report.ID,
	)

	sendJSON(w, http.StatusAccepted, map[string]interface{}{
		"report_id": report.ID,
	})
}

// -----------------------------------------------------------------------------
// 공통
// -----------------------------------------------------------------------------

func (a *API) verifyWebhookSecret(r *http.Request) bool {
	configured := a.deps.WebhookSecret
	if configured == "" {
		return false
	}
	got := r.Header.Get("X-AI-Service-Reporter-Secret")
	return got != "" && got == configured
}
