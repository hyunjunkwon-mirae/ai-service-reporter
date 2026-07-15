package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
)

// =============================================================================
// 구독 수신 등록 — 저장 / 수정 / 조회 / 삭제
// =============================================================================

// GetSubscriptionResponse는 RHS 패널 초기 로드시 한 번에 받는 응답입니다.
type GetSubscriptionResponse struct {
	// 구독 존재 여부. false면 사용자가 아직 등록 안 한 상태
	Subscribed bool `json:"subscribed"`
	// 구독 상세 — Subscribed=false 이면 nil
	Subscription *Subscription `json:"subscription,omitempty"`
	// 마스터 (UI가 칩을 렌더링할 수 있도록 함께 보냄)
	ResourceGroups []*ResourceGroup `json:"resource_groups"`
	// 발송 시간 (UI에 표시용, read-only)
	DeliveryTime string `json:"delivery_time"`
}

// handleGetSubscription — GET /api/subscription
func (a *API) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-Id")

	if a.deps.SubscriptionStore == nil || a.deps.ResourceGroupStore == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB가 아직 준비되지 않았습니다.")
		return
	}

	groups, err := a.deps.ResourceGroupStore.ListActive()
	if err != nil {
		a.deps.PluginAPI.LogError("Failed to list resource groups", "error", err.Error())
		sendError(w, http.StatusInternalServerError, "RG_LIST_FAILED", "리소스그룹 조회 실패")
		return
	}

	sub, err := a.deps.SubscriptionStore.GetByUserID(userID)
	if err != nil {
		a.deps.PluginAPI.LogError("Failed to get subscription", "userID", maskUserID(userID), "error", err.Error())
		sendError(w, http.StatusInternalServerError, "SUB_GET_FAILED", "구독 조회 실패")
		return
	}

	resp := GetSubscriptionResponse{
		Subscribed:     sub != nil,
		Subscription:   sub,
		ResourceGroups: groups,
		DeliveryTime:   a.currentDeliveryTime(),
	}
	sendJSON(w, http.StatusOK, resp)
}

// PutSubscriptionRequest는 클라이언트가 보낼 저장 요청입니다.
type PutSubscriptionRequest struct {
	Active         bool     `json:"active"`
	ChannelID      string   `json:"channel_id,omitempty"`   // ""이면 본인 DM
	ResourceGroups []string `json:"resource_groups"`        // 빈 배열 = 전체 수신
}

// handlePutSubscription — PUT /api/subscription
// 구독 저장 = upsert. 저장 성공 후 봇이 본인 DM으로 변경 요약을 전송합니다.
func (a *API) handlePutSubscription(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-Id")

	if a.deps.SubscriptionStore == nil || a.deps.ResourceGroupStore == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB가 아직 준비되지 않았습니다.")
		return
	}

	var req PutSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "INVALID_BODY", "요청 본문 파싱 실패")
		return
	}
	defer r.Body.Close()

	// 1) 리소스그룹 코드 검증
	validCodes, err := a.deps.ResourceGroupStore.AllValidCodes()
	if err != nil {
		sendError(w, http.StatusInternalServerError, "RG_LIST_FAILED", "리소스그룹 마스터 조회 실패")
		return
	}
	// 중복 제거 + 정렬
	dedup := make(map[string]struct{})
	for _, c := range req.ResourceGroups {
		if _, ok := validCodes[c]; !ok {
			sendError(w, http.StatusBadRequest, "INVALID_RESOURCE_GROUP",
				fmt.Sprintf("알 수 없는 리소스그룹 코드: %s", c))
			return
		}
		dedup[c] = struct{}{}
	}
	normalized := make([]string, 0, len(dedup))
	for c := range dedup {
		normalized = append(normalized, c)
	}
	sort.Strings(normalized)

	// 2) 채널 ID 지정 시 봇이 멤버인지 확인
	if req.ChannelID != "" {
		if a.deps.Bot == nil {
			sendError(w, http.StatusServiceUnavailable, "BOT_NOT_READY", "봇이 준비되지 않았습니다.")
			return
		}
		isMember, mErr := a.deps.Bot.IsBotInChannel(req.ChannelID)
		if mErr != nil {
			a.deps.PluginAPI.LogWarn("Channel membership check failed",
				"channelID", req.ChannelID, "error", mErr.Error())
			sendError(w, http.StatusBadRequest, "CHANNEL_NOT_FOUND", "채널을 찾을 수 없습니다.")
			return
		}
		if !isMember {
			sendError(w, http.StatusBadRequest, "BOT_NOT_IN_CHANNEL",
				"봇이 해당 채널의 멤버가 아닙니다. 관리자에게 봇을 채널에 초대해달라고 요청해주세요.")
			return
		}
	}

	// 3) Upsert
	sub := &Subscription{
		UserID:         userID,
		Active:         req.Active,
		ChannelID:      req.ChannelID,
		ResourceGroups: normalized,
	}
	if err := a.deps.SubscriptionStore.Upsert(sub); err != nil {
		a.deps.PluginAPI.LogError("Failed to upsert subscription",
			"userID", maskUserID(userID), "error", err.Error())
		sendError(w, http.StatusInternalServerError, "SUB_SAVE_FAILED", "구독 저장 실패")
		return
	}

	// 4) 변경 요약 DM 발송 (실패해도 저장 자체는 성공으로 반환)
	if a.deps.Bot != nil {
		if err := a.deps.Bot.SendChangeSummary(userID, sub.Active, sub.ChannelID, sub.ResourceGroups, a.currentDeliveryTime()); err != nil {
			a.deps.PluginAPI.LogWarn("Failed to send change summary DM",
				"userID", maskUserID(userID), "error", err.Error())
		}
	}

	// 5) 갱신된 상태 반환
	groups, _ := a.deps.ResourceGroupStore.ListActive()
	sendJSON(w, http.StatusOK, GetSubscriptionResponse{
		Subscribed:     true,
		Subscription:   sub,
		ResourceGroups: groups,
		DeliveryTime:   a.currentDeliveryTime(),
	})
}

// handleDeleteSubscription — DELETE /api/subscription
// soft delete (deleteat 갱신). UI에서는 토글 OFF + 저장으로도 같은 효과.
func (a *API) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-Id")

	if a.deps.SubscriptionStore == nil {
		sendError(w, http.StatusServiceUnavailable, "DB_NOT_READY", "DB가 아직 준비되지 않았습니다.")
		return
	}

	if err := a.deps.SubscriptionStore.SoftDeleteByUserID(userID); err != nil {
		a.deps.PluginAPI.LogError("Failed to soft delete subscription",
			"userID", maskUserID(userID), "error", err.Error())
		sendError(w, http.StatusInternalServerError, "SUB_DELETE_FAILED", "구독 해제 실패")
		return
	}

	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "구독이 해제되었습니다.",
	})
}

// maskUserID는 로그에서 사용자 ID를 앞 8자만 노출합니다.
func maskUserID(userID string) string {
	if len(userID) <= 8 {
		return userID
	}
	return userID[:8] + "..."
}
