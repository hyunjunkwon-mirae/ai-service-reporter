// =============================================================================
// 플러그인 공통 상수
//
// 서버 쪽 SummaryResourceGroupCode (server/api/webhook_routes.go) 와 동일 값.
// 양쪽 값이 항상 동기화되어야 함.
// =============================================================================

// SUMMARY 행은 일 단위 글로벌 분석 요약을 담는 sentinel 코드.
// · 마스터(resourcegroups) 테이블에는 시드되지 않음
// · 사용자 RHS 칩 그리드 / admin 화면에서 모두 표출 제외
// · admin이 수동 입력 시도 시에도 거부
export const RESERVED_SUMMARY_CODE = "SUMMARY";
