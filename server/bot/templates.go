package bot

import (
	"fmt"
	"sort"
	"strings"
)

// =============================================================================
// 메시지 템플릿 렌더러
//
// 발송 메시지 구조:
//   [헤더 (인사말, {user.nickname} 치환)]
//   [Summary 섹션 — 없으면 생략]
//   [TR Statistics Summary]
//     - TR 호출 건수 증가(절댓값) TOP 5  (call_count_rank ASC, 상위 N)
//        | 서비스ID | 서비스명 | 주간 평균 호출수 | 2 영업일 전 호출수 |
//     - TR 호출 건수 증가율(상댓값) TOP 5  (call_count_pct_rank ASC, 상위 N)
//        | 서비스ID | 서비스명 | 호출 건수 증가율 |
//     - TR 실행 시간 증가 TOP 5  (latency_diff_rank ASC, 상위 N)
//        | 서비스ID | 서비스명 | Latency 변화 |
//
// 표 안에 데이터가 0건이면 "오늘은 증가 건수가 없습니다." 같은 안내 메시지를 출력.
// 모든 컬럼은 좌측 정렬.
// =============================================================================

// 기본 템플릿 — 설정값이 비어있을 때 fallback.
const (
	DefaultHeaderTemplate     = "안녕하세요! {user.nickname}님!\n{date} 어플리케이션 모니터링 리포트를 전달드립니다.\n\n베타 운영중입니다. 오류나 개선 아이디어가 있으시면 언제든 메타모스트 관리자에게 연락 부탁드립니다!.\n"
	DefaultNoDataTemplate     = "안녕하세요! {user.nickname}님!\n{date}은 전달드릴 분석 결과가 없습니다. 👍\n\n_AI Service Reporter — 매일 {delivery_time} KST 자동 발송_"
	DefaultTableHeaderCall    = "TR 호출 건수 증가(절댓값) TOP 5"
	DefaultTableHeaderCallPct = "TR 호출 건수 증가율(상댓값) TOP 5"
	DefaultTableHeaderLatency = "TR 실행 시간 증가 TOP 5"
	DefaultTableNoDataMessage = "오늘은 증가 건수가 없습니다."
	DefaultTableTopN          = 5
)

// =============================================================================
// 도메인 타입
// =============================================================================

// Anomaly 는 LLM 이 보내는 단일 이상 항목입니다.
//
// 한 TR 이 호출수·증가율·지연 세 표에 모두 등장할 수 있으므로 case 필드는
// 사용하지 않습니다. 어느 표에 들어갈지는 *_rank 필드의 존재 여부로 결정됩니다.
//
// 랭크값은 AIOps 서버가 전체 서비스 풀(업무영역 무관) 기준으로 매겨 보냅니다.
// 플러그인은 사용자가 선택한 업무영역들로 필터링한 뒤 각 랭크별 상위 N건을 표시.
type Anomaly struct {
	TrCode string `json:"tr_code"`
	TrName string `json:"tr_name"`

	// 호출수 — 호출수 절댓값 / 증가율 표에서 사용
	CallCount     *int64   `json:"call_count,omitempty"`
	BaselineCount *int64   `json:"baseline_count,omitempty"`
	DiffCount     *int64   `json:"diff_count,omitempty"`
	DiffPct       *float64 `json:"diff_pct,omitempty"`

	// 응답시간 — Latency 표에서 사용 (단위: ms)
	AvgTimeMs      *float64 `json:"avg_time_ms,omitempty"`
	BaselineAvgMs  *float64 `json:"baseline_avg_ms,omitempty"`
	DiffMs         *float64 `json:"diff_ms,omitempty"`
	LatencyDiffPct *float64 `json:"latency_diff_pct,omitempty"`

	// 랭크 — AIOps 가 전체 풀 기준으로 매김. 1 이 가장 높음. 0/nil = 해당 표 미해당.
	CallCountRank    *int `json:"call_count_rank,omitempty"`
	CallCountPctRank *int `json:"call_count_pct_rank,omitempty"`
	LatencyDiffRank  *int `json:"latency_diff_rank,omitempty"`
}

// RenderConfig 는 본문 렌더링에 사용할 템플릿 묶음입니다.
// 설정값이 비어있으면 Default* 상수로 폴백됩니다.
type RenderConfig struct {
	HeaderTemplate     string
	TableHeaderCall    string
	TableHeaderCallPct string
	TableHeaderLatency string
	TableNoDataMessage string
	TableTopN          int
}

func (c RenderConfig) resolveOrDefault() RenderConfig {
	if c.HeaderTemplate == "" {
		c.HeaderTemplate = DefaultHeaderTemplate
	}
	if c.TableHeaderCall == "" {
		c.TableHeaderCall = DefaultTableHeaderCall
	}
	if c.TableHeaderCallPct == "" {
		c.TableHeaderCallPct = DefaultTableHeaderCallPct
	}
	if c.TableHeaderLatency == "" {
		c.TableHeaderLatency = DefaultTableHeaderLatency
	}
	if c.TableNoDataMessage == "" {
		c.TableNoDataMessage = DefaultTableNoDataMessage
	}
	if c.TableTopN <= 0 {
		c.TableTopN = DefaultTableTopN
	}
	return c
}

// =============================================================================
// 메시지 조립 — 발송 시점에 호출
// =============================================================================

// RenderUserMessage 는 사용자에게 보낼 최종 메시지를 조립합니다.
//
//   - summary    : SUMMARY 리포트의 content. 빈 문자열이면 Summary 섹션 생략.
//   - anomalies  : 사용자가 선택한 업무영역들의 모든 anomaly 를 평탄화한 슬라이스.
//   - date       : YYYY-MM-DD (헤더 치환용)
//   - cfg        : 템플릿/표 라벨/TopN 설정
//
// {user.nickname} 치환은 bot.go 의 replaceNickname() 에서 발송 직전에 수행되므로
// 여기서는 그대로 통과시킵니다.
func RenderUserMessage(summary string, anomalies []Anomaly, date string, cfg RenderConfig) string {
	cfg = cfg.resolveOrDefault()

	var sb strings.Builder

	// 1) 헤더
	header := strings.ReplaceAll(cfg.HeaderTemplate, "{date}", date)
	sb.WriteString(header)
	if !strings.HasSuffix(header, "\n") {
		sb.WriteString("\n")
	}

	// 2) Summary 섹션 (있을 때만)
	if strings.TrimSpace(summary) != "" {
		sb.WriteString("\n#### 📋 Summary\n")
		sb.WriteString(strings.TrimSpace(summary))
		sb.WriteString("\n")
	}

	// 3) TR Statistics Summary
	sb.WriteString("\n#### 📊 TR Statistics Summary\n")

	// 각 표 앞에 가로줄(---)로 간격을 주어 시각적으로 표 구분.
	// 표 자체의 헤더 정렬 구분자와 혼동되지 않도록 앞뒤 빈 줄 확실히.
	sb.WriteString("\n---\n\n**" + cfg.TableHeaderCall + "**\n\n")
	sb.WriteString(renderTopTable(anomalies, rankByCall, tableKindCall, cfg.TableTopN, cfg.TableNoDataMessage))

	sb.WriteString("\n---\n\n**" + cfg.TableHeaderCallPct + "**\n\n")
	sb.WriteString(renderTopTable(anomalies, rankByCallPct, tableKindCallPct, cfg.TableTopN, cfg.TableNoDataMessage))

	sb.WriteString("\n---\n\n**" + cfg.TableHeaderLatency + "**\n\n")
	sb.WriteString(renderTopTable(anomalies, rankByLatency, tableKindLatency, cfg.TableTopN, cfg.TableNoDataMessage))

	return sb.String()
}

// RenderNoDataMessage 는 폴백 메시지를 렌더링합니다. (Summary 도 anomaly 도 없을 때)
func RenderNoDataMessage(template, date, deliveryTime string) string {
	if template == "" {
		template = DefaultNoDataTemplate
	}
	r := strings.NewReplacer(
		"{date}", date,
		"{delivery_time}", deliveryTime,
	)
	return r.Replace(template)
}

// =============================================================================
// 표 렌더링
// =============================================================================

// 랭크 추출자 — *int 필드 셋 중 하나를 골라서 정렬 키로 사용
type rankExtractor func(a Anomaly) (rank int, ok bool)

func rankByCall(a Anomaly) (int, bool) {
	if a.CallCountRank == nil || *a.CallCountRank <= 0 {
		return 0, false
	}
	return *a.CallCountRank, true
}
func rankByCallPct(a Anomaly) (int, bool) {
	if a.CallCountPctRank == nil || *a.CallCountPctRank <= 0 {
		return 0, false
	}
	return *a.CallCountPctRank, true
}
func rankByLatency(a Anomaly) (int, bool) {
	if a.LatencyDiffRank == nil || *a.LatencyDiffRank <= 0 {
		return 0, false
	}
	return *a.LatencyDiffRank, true
}

// 표 종류 — 표마다 컬럼 구성이 달라서 분기에 사용.
type tableKind int

const (
	tableKindCall     tableKind = iota // 호출 건수 증가(절댓값): 서비스ID | 서비스명 | 주간 평균 호출수 | 2 영업일 전 호출수
	tableKindCallPct                   // 호출 건수 증가율(상댓값): 서비스ID | 서비스명 | 호출 건수 증가율
	tableKindLatency                   // 실행 시간 증가:           서비스ID | 서비스명 | Latency 변화
)

// renderTopTable 은 anomalies 에서 해당 랭크가 있는 것만 골라 상위 topN 을
// markdown 표로 렌더링합니다. 0건이면 noDataMessage 만 출력.
// 표 종류에 따라 컬럼 구성이 다름.
func renderTopTable(anomalies []Anomaly, getRank rankExtractor, kind tableKind, topN int, noDataMessage string) string {
	type ranked struct {
		rank int
		a    Anomaly
	}
	picked := make([]ranked, 0, len(anomalies))
	for _, a := range anomalies {
		if r, ok := getRank(a); ok {
			picked = append(picked, ranked{rank: r, a: a})
		}
	}
	if len(picked) == 0 {
		return noDataMessage + "\n"
	}
	sort.Slice(picked, func(i, j int) bool {
		return picked[i].rank < picked[j].rank
	})
	if len(picked) > topN {
		picked = picked[:topN]
	}

	var sb strings.Builder
	switch kind {
	case tableKindCall:
		sb.WriteString("| 서비스ID | 서비스명 | 주간 평균 호출수 | 2 영업일 전 호출수 |\n")
		sb.WriteString("|:---|:---|:---|:---|\n")
	case tableKindCallPct:
		sb.WriteString("| 서비스ID | 서비스명 | 호출 건수 증가율 |\n")
		sb.WriteString("|:---|:---|:---|\n")
	case tableKindLatency:
		sb.WriteString("| 서비스ID | 서비스명 | Latency 변화 |\n")
		sb.WriteString("|:---|:---|:---|\n")
	}
	for _, p := range picked {
		sb.WriteString(renderRow(p.a, kind))
	}
	return sb.String()
}

// renderRow 는 anomaly 1건을 표 종류에 따라 한 행으로 변환합니다.
//
// 표마다 컬럼 구성이 다름:
//   tableKindCall    → | 서비스ID | 서비스명 | 주간 평균 호출수 | 2 영업일 전 호출수 |
//   tableKindCallPct → | 서비스ID | 서비스명 | 호출 건수 증가율 |
//   tableKindLatency → | 서비스ID | 서비스명 | Latency 변화 |
//
// Latency 변화 포맷: "360ms (+100%, +180ms)"
//   · AvgTimeMs / LatencyDiffPct / DiffMs 모두 없으면 "-"
func renderRow(a Anomaly, kind tableKind) string {
	// 서비스ID = tr_code. 비어 있으면 tr_name 으로 폴백 (안전망)
	id := a.TrCode
	if strings.TrimSpace(id) == "" {
		id = a.TrName
	}
	// 서비스명 = tr_name. 비어 있으면 "-" 로 표기
	name := strings.TrimSpace(a.TrName)
	if name == "" {
		name = "-"
	}
	switch kind {
	case tableKindCall:
		return fmt.Sprintf("| %s | %s | %s | %s |\n",
			id, name, formatInt(a.BaselineCount), formatInt(a.CallCount))
	case tableKindCallPct:
		return fmt.Sprintf("| %s | %s | %s |\n",
			id, name, formatPct(a.DiffPct))
	case tableKindLatency:
		return fmt.Sprintf("| %s | %s | %s |\n",
			id, name, formatLatencyCell(a))
	}
	return ""
}

func formatLatencyCell(a Anomaly) string {
	if a.AvgTimeMs == nil && a.DiffMs == nil && a.LatencyDiffPct == nil {
		return "-"
	}
	avg := "-"
	if a.AvgTimeMs != nil {
		avg = formatInt64WithCommas(int64(*a.AvgTimeMs)) + "ms"
	}
	pct := "0%"
	if a.LatencyDiffPct != nil {
		pct = formatPctFloat(*a.LatencyDiffPct)
	}
	diff := "0ms"
	if a.DiffMs != nil {
		v := int64(*a.DiffMs)
		if v >= 0 {
			diff = "+" + formatInt64WithCommas(v) + "ms"
		} else {
			diff = formatInt64WithCommas(v) + "ms"
		}
	}
	return fmt.Sprintf("%s (%s, %s)", avg, pct, diff)
}

// =============================================================================
// 숫자 포매팅
// =============================================================================

func formatInt(v *int64) string {
	if v == nil {
		return "0"
	}
	return formatInt64WithCommas(*v)
}

// formatPct 는 *float64 (%) 를 "+128%" / "+26,600%" / "0%" / "-12%" 형식으로 변환.
func formatPct(v *float64) string {
	if v == nil {
		return "0%"
	}
	return formatPctFloat(*v)
}

// formatPctFloat 는 %를 정수로 반올림하여 천단위 콤마 + 부호와 함께 반환.
// 예:  128    → "+128%"
//      26600  → "+26,600%"
//      0      → "0%"
//     -1234   → "-1,234%"
func formatPctFloat(v float64) string {
	n := int64(v)
	if n > 0 {
		return "+" + formatInt64WithCommas(n) + "%"
	}
	// 음수/0 은 formatInt64WithCommas 가 알아서 부호 처리
	return formatInt64WithCommas(n) + "%"
}

func formatInt64WithCommas(n int64) string {
	if n < 0 {
		return "-" + formatInt64WithCommas(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return formatInt64WithCommas(n/1000) + fmt.Sprintf(",%03d", n%1000)
}
