package api

import (
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

// =============================================================================
// 도메인 모델
// =============================================================================

// Subscription 은 사용자 구독 정보입니다.
type Subscription struct {
	ID             string   `json:"id"`
	UserID         string   `json:"user_id"`
	Active         bool     `json:"active"`
	ChannelID      string   `json:"channel_id,omitempty"`   // ""이면 본인 DM
	ResourceGroups []string `json:"resource_groups"`        // 빈 배열 = 전체 수신
	CreateAt       int64    `json:"create_at"`
	UpdateAt       int64    `json:"update_at"`
	DeleteAt       int64    `json:"delete_at"`
}

// ResourceGroup 은 업무영역 마스터입니다.
type ResourceGroup struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Definition *string `json:"definition,omitempty"`
	SortOrder   int     `json:"sort_order"`
	Active      bool    `json:"active"`
}

// Report 는 LLM이 보낸 일별/업무영역별 리포트입니다.
type Report struct {
	ID                string `json:"id"`
	ReportDate        string `json:"report_date"`         // YYYY-MM-DD
	ResourceGroupCode string `json:"resource_group_code"`
	WindowDays        int    `json:"window_days"`
	Content           string `json:"content"`             // 봇 발송 본문
	RawData           []byte `json:"-"`                   // jsonb
	CreateAt          int64  `json:"create_at"`
	UpdateAt          int64  `json:"update_at"`
}

// DeliveryLog 는 사용자별 발송 결과입니다.
type DeliveryLog struct {
	ID             string  `json:"id"`
	UserID         string  `json:"user_id"`
	SubscriptionID string  `json:"subscription_id"`
	ReportID       string  `json:"report_id"`
	ChannelID      *string `json:"channel_id,omitempty"`
	Status         string  `json:"status"`           // pending / sent / failed
	RetryCount     int     `json:"retry_count"`
	Error          *string `json:"error,omitempty"`
	SentAt         *int64  `json:"sent_at,omitempty"`
	CreateAt       int64   `json:"create_at"`
	UpdateAt       int64   `json:"update_at"`
}

// 발송 상태 상수
const (
	DeliveryStatusPending = "pending"
	DeliveryStatusSent    = "sent"
	DeliveryStatusFailed  = "failed"
)

// =============================================================================
// 유틸리티
// =============================================================================

// NewID는 Mattermost 호환 26자 ID를 생성합니다. (nanoid)
func NewID() string {
	id, _ := gonanoid.Generate("0123456789abcdefghijklmnopqrstuvwxyz", 26)
	return id
}

// NowEpochMs는 현재 시각을 epoch milliseconds 로 반환합니다.
func NowEpochMs() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}
