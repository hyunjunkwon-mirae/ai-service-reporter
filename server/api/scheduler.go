package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/miraeasset/ai-service-reporter-plugin/server/bot"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

// =============================================================================
// DeliveryScheduler
//
//  · 매일 1회, plugin.json 설정 DeliveryTime (기본 10:00) KST 에 트리거
//  · 활성 구독자 모두에 대해 그날의 리포트를 매칭하여 봇 DM 발송
//  · 발송 결과는 ai_service_reporter_delivery_log 에 기록 (sent / failed / pending)
//  · 5분마다 retrycount < 3 인 failed 로그를 재시도
//  · 클러스터 환경: KV Store mutex 로 단일 노드만 실행
//
// 참고: reference plugin ReminderScheduler 와 같은 패턴 (Start/Stop, goroutine + ticker).
// =============================================================================

const (
	// 매분 체크해서 발송 시간에 도달했는지 확인 (cron 라이브러리 미사용 → 자체 구현)
	scanInterval = 1 * time.Minute

	// 재시도 큐 확인 간격
	retryInterval = 5 * time.Minute

	// 최대 재시도 횟수
	maxRetries = 3

	// 클러스터 mutex TTL (초). 발송 처리에 충분한 시간 부여.
	clusterMutexTTLSeconds = 3600

	// reports 의 가장 최근 reportdate 가 실행일로부터 며칠 이상 떨어져 있으면
	// "데이터가 너무 오래됨" 으로 보고 발송 skip + 관리자 알림.
	// AIOps 가 며칠 동안 데이터를 안 보낸 경우의 안전망.
	maxReportAgeDays = 7
)

// seoulLocation은 한국 시간대입니다.
var seoulLocation = mustLoadLocation("Asia/Seoul")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// fallback: KST = UTC+9
		return time.FixedZone("KST", 9*3600)
	}
	return loc
}

// DeliveryScheduler는 매일 발송과 재시도를 관리합니다.
type DeliveryScheduler struct {
	pluginAPI         plugin.API
	bot               *bot.AIServiceReporterBot
	subscriptionStore *SubscriptionStore
	reportStore       *ReportStore
	deliveryLogStore  *DeliveryLogStore

	deliveryTime    string // "HH:MM"
	adminChannelID  string
	testModeEnabled bool // true 면 5분마다 deliverAll 호출 (검증용)

	// 매 호출마다 최신 no-data 템플릿을 반환 (설정 변경 즉시 반영)
	getNoDataTemplate func() string

	// 매 호출마다 최신 헤더/표 라벨 템플릿을 반환 (설정 변경 즉시 반영)
	getRenderConfig func() bot.RenderConfig

	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
	running  bool

	// 같은 날에 중복 트리거 방지
	lastRunDate string // "YYYY-MM-DD" in KST
}

// NewDeliveryScheduler는 새로운 스케줄러를 생성합니다.
func NewDeliveryScheduler(
	pluginAPI plugin.API,
	aiReporterBot *bot.AIServiceReporterBot,
	subStore *SubscriptionStore,
	reportStore *ReportStore,
	logStore *DeliveryLogStore,
	deliveryTime string,
	adminChannelID string,
	testModeEnabled bool,
	getNoDataTemplate func() string,
	getRenderConfig func() bot.RenderConfig,
) *DeliveryScheduler {
	if deliveryTime == "" {
		deliveryTime = "10:00"
	}
	return &DeliveryScheduler{
		pluginAPI:         pluginAPI,
		bot:               aiReporterBot,
		subscriptionStore: subStore,
		reportStore:       reportStore,
		deliveryLogStore:  logStore,
		deliveryTime:      deliveryTime,
		adminChannelID:    adminChannelID,
		testModeEnabled:   testModeEnabled,
		getNoDataTemplate: getNoDataTemplate,
		getRenderConfig:   getRenderConfig,
		stopChan:          make(chan struct{}),
	}
}

// Start는 스케줄러를 시작합니다.
func (s *DeliveryScheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopChan = make(chan struct{})
	s.mu.Unlock()

	s.wg.Add(3)
	go s.runMainLoop()
	go s.runRetryLoop()
	go s.runTestLoop()

	s.pluginAPI.LogInfo("Delivery scheduler started",
		"deliveryTime", s.deliveryTime,
		"tz", "Asia/Seoul",
		"testMode", s.testModeEnabled)
}

// Stop은 스케줄러를 중지합니다.
func (s *DeliveryScheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopChan)
	s.mu.Unlock()

	s.wg.Wait()
	s.pluginAPI.LogInfo("Delivery scheduler stopped")
}

// UpdateStores는 설정변경(dev↔prod) 등으로 DB가 재초기화될 때 호출됩니다.
func (s *DeliveryScheduler) UpdateStores(sub *SubscriptionStore, rep *ReportStore, log *DeliveryLogStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscriptionStore = sub
	s.reportStore = rep
	s.deliveryLogStore = log
}

// --- mutex 보호 상태 읽기 헬퍼 -----------------------------------------------
// scheduler 의 deliveryTime / testModeEnabled / adminChannelID 는 Restart 시
// 갱신되므로 매번 mutex 보호 하에 읽어야 함. 짧은 critical section 을 코드
// 곳곳에서 반복 작성하지 않도록 1줄 헬퍼로 정리.

func (s *DeliveryScheduler) getDeliveryTime() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deliveryTime
}

func (s *DeliveryScheduler) getAdminChannelID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.adminChannelID
}

func (s *DeliveryScheduler) getTestMode() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.testModeEnabled
}

func (s *DeliveryScheduler) getLastRunDate() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRunDate
}
// ---------------------------------------------------------------------------

// Restart 는 발송시간/테스트모드 변경 시 호출되어 스케줄러를 안전하게 재시작합니다.
//
//   · Stop 으로 기존 ticker 들 정리 → 새 값으로 갱신 → Start 로 재시작.
//   · 같은 인스턴스를 재사용하므로 lastRunDate 가 보존됨 (같은 날 중복 발송 방지).
//   · Stop ~ Start 사이에 보통 1초 미만의 공백이 있으나, ticker 가 분 단위라 영향 거의 없음.
//   · stopChan 은 Start 가 새로 만들어주므로 여기서는 필드 갱신만 수행.
func (s *DeliveryScheduler) Restart(deliveryTime, adminChannelID string, testModeEnabled bool) {
	s.Stop()

	s.mu.Lock()
	if deliveryTime == "" {
		deliveryTime = "10:00"
	}
	s.deliveryTime = deliveryTime
	s.adminChannelID = adminChannelID
	s.testModeEnabled = testModeEnabled
	s.mu.Unlock()

	s.Start()
}

// =============================================================================
// 메인 루프 — 매분 시각 비교
// =============================================================================

func (s *DeliveryScheduler) runMainLoop() {
	defer s.wg.Done()

	// 첫 분 정각까지 대기
	wait := timeUntilNextMinute()
	select {
	case <-time.After(wait):
	case <-s.stopChan:
		return
	}

	// 첫 체크
	s.checkAndDeliver()

	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.checkAndDeliver()
		case <-s.stopChan:
			return
		}
	}
}

// checkAndDeliver는 KST 기준 현재 시각이 발송 시간과 일치하는지 확인하고,
// 일치하면 그날 발송을 트리거합니다.
func (s *DeliveryScheduler) checkAndDeliver() {
	now := time.Now().In(seoulLocation)
	hhmm := fmt.Sprintf("%02d:%02d", now.Hour(), now.Minute())

	s.mu.Lock()
	targetTime := s.deliveryTime
	lastRun := s.lastRunDate
	testMode := s.testModeEnabled
	today := now.Format("2006-01-02")
	s.mu.Unlock()

	// 5분마다 heartbeat 로그 (스케줄러가 살아있는지 확인용)
	if now.Minute()%5 == 0 {
		s.pluginAPI.LogInfo("Scheduler heartbeat",
			"now", hhmm,
			"target", targetTime,
			"lastRun", lastRun,
			"testMode", testMode)
	}

	if hhmm != targetTime {
		return
	}
	if lastRun == today {
		// 이미 오늘 실행 완료
		return
	}

	// 클러스터 mutex — 같은 시각 다른 노드 중복 방지
	mutexKey := "ai_service_reporter_daily_delivery_" + today
	if !tryAcquireMutex(s.pluginAPI, mutexKey, clusterMutexTTLSeconds) {
		s.pluginAPI.LogInfo("Daily delivery skipped — another node is handling", "date", today)
		s.markDateRun(today)
		return
	}

	s.pluginAPI.LogInfo("Daily delivery triggered",
		"date", today, "time", hhmm)
	s.deliverAll(today)
	s.markDateRun(today)
}

// runTestLoop은 테스트 모드 ON 일 때 5분 간격으로 deliverAll 을 호출합니다.
// 멱등성으로 동일 (user, report) 조합 중복 발송은 방지됩니다.
// (단, 무데이터 폴백 DM 은 5분마다 발송될 수 있음 — 도움말에 명시.)
func (s *DeliveryScheduler) runTestLoop() {
	defer s.wg.Done()

	// 활성화 직후 5초 뒤 첫 회 실행 (관리자 검증 편의용),
	// 이후 5분 간격으로 반복. testModeEnabled=false 면 skip.
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			if s.getTestMode() {
				today := time.Now().In(seoulLocation).Format("2006-01-02")
				s.pluginAPI.LogInfo("Test mode trigger — running deliverAll", "date", today)
				s.deliverAll(today)
			}
			timer.Reset(5 * time.Minute)
		case <-s.stopChan:
			return
		}
	}
}

func (s *DeliveryScheduler) markDateRun(date string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRunDate = date
}

// =============================================================================
// 일일 발송 핵심 로직
// =============================================================================

// deliverAll은 발송 트리거 시점에 모든 활성 구독자에게 발송합니다.
//
// 알고리즘:
//   runDate    = 실행일 (멱등성/mutex/로그용)
//   targetDate = reports 테이블의 MAX(reportdate). AIOps 가 적재한 가장 최근 분석일.
//                → 영업일 계산은 plugin 이 안 하고 AIOps 가 보낸 데이터 기준.
//
//   가드:
//     · reports 가 1건도 없으면 → 모든 활성 구독자에게 폴백 메시지 (displayDate = runDate)
//     · max 가 runDate 로부터 maxReportAgeDays 일보다 오래되면 → skip + 관리자 알림
//
//   targetDate 리포트 로드:
//     - resourcegroupcode='SUMMARY' 행 → summaryReport (일 단위 글로벌)
//     - 그 외 → groupReports[code] (업무영역별 anomaly 보유)
//   for each active subscription S:
//     매칭 영역 리포트들의 anomalies 를 평탄화 → 1통의 메시지 조립
//     · Summary 있으면 Summary 섹션 포함, 없으면 생략
//     · 매칭 영역도 없고 Summary 도 없으면 NoData 폴백
//     · 매칭 영역 없는데 Summary 만 있으면 Summary + "오늘은 증가 건수가 없습니다" 표
func (s *DeliveryScheduler) deliverAll(runDate string) {
	if s.subscriptionStore == nil || s.reportStore == nil || s.deliveryLogStore == nil || s.bot == nil {
		s.pluginAPI.LogError("Cannot deliver — dependencies not ready")
		return
	}

	subs, err := s.subscriptionStore.ListActive()
	if err != nil {
		s.pluginAPI.LogError("Failed to list active subscriptions", "error", err.Error())
		return
	}
	if len(subs) == 0 {
		s.pluginAPI.LogInfo("No active subscriptions", "runDate", runDate)
		return
	}

	// reports 테이블의 가장 최근 reportdate 를 발송 대상일로 사용
	targetDate, found, mdErr := s.reportStore.MaxReportDate()
	if mdErr != nil {
		s.pluginAPI.LogError("Failed to get MAX(reportdate)", "error", mdErr.Error())
		return
	}

	if !found {
		// reports 가 1건도 없음 → 옵션 A: 모든 활성 구독자에게 폴백 발송 (displayDate = runDate)
		s.pluginAPI.LogWarn("No reports available — sending fallback to all subscribers",
			"runDate", runDate)
		nodataCount, failCount := 0, 0
		for _, sub := range subs {
			if s.deliverNoData(sub, runDate) {
				nodataCount++
			} else {
				failCount++
			}
		}
		s.pluginAPI.LogInfo("Fallback delivery completed (no reports)",
			"runDate", runDate, "subscribers", len(subs),
			"nodata", nodataCount, "failed", failCount)
		return
	}

	// 데이터가 너무 오래된 경우 → skip + 관리자 알림 (옵션 B)
	if ageDays, ok := daysBetween(targetDate, runDate); ok && ageDays > maxReportAgeDays {
		adminCh := s.getAdminChannelID()
		s.pluginAPI.LogWarn("Latest report too old — skipping delivery",
			"runDate", runDate, "targetDate", targetDate,
			"ageDays", ageDays, "threshold", maxReportAgeDays)
		if adminCh != "" {
			_ = s.bot.NotifyAdminChannel(adminCh,
				fmt.Sprintf("⚠️ AI Service Reporter: 가장 최근 데이터(%s)가 %d일 이상 지나 발송을 건너뜁니다. AIOps 적재 상태를 확인해 주세요.",
					targetDate, ageDays))
		}
		return
	}

	allTodayReports, err := s.reportStore.ListByDate(targetDate)
	if err != nil {
		s.pluginAPI.LogError("Failed to list target-date reports",
			"targetDate", targetDate, "error", err.Error())
		return
	}

	// Summary 분리 + 영역별 인덱싱 (공통 헬퍼)
	summaryReport, byGroup := indexReports(allTodayReports)

	// 테스트 모드 여부 — Restart 시 갱신되므로 필드 직접 참조.
	// 테스트 모드면 멱등성 체크(ExistsForReport) 무시하고 매번 발송.
	testMode := s.getTestMode()

	sentCount, failCount, nodataCount := 0, 0, 0
	for _, sub := range subs {
		// 매칭되는 영역 리포트 결정 (공통 헬퍼)
		matched := matchReportsForUser(sub, byGroup)

		// Summary 도 없고 매칭 영역도 없으면 폴백
		if summaryReport == nil && len(matched) == 0 {
			if s.deliverNoData(sub, targetDate) {
				nodataCount++
			} else {
				failCount++
			}
			continue
		}

		// 멱등성 체크 — Summary + 영역 리포트들 중 sent 가 하나라도 있으면 이미 발송됨으로 간주.
		// (메시지 단위가 사용자별 1통이므로 영역 단위 중복 발송 방지)
		// 테스트 모드(5분 간격)일 때는 검증 편의를 위해 이 체크를 우회하고 매번 발송한다.
		if !testMode {
			alreadySent := false
			var allReports []*Report
			if summaryReport != nil {
				allReports = append(allReports, summaryReport)
			}
			allReports = append(allReports, matched...)
			for _, r := range allReports {
				ok, _ := s.deliveryLogStore.ExistsForReport(sub.UserID, r.ID)
				if ok {
					alreadySent = true
					break
				}
			}
			if alreadySent {
				continue
			}
		}

		if s.deliverToUser(sub, summaryReport, matched, targetDate) {
			sentCount++
		} else {
			failCount++
		}
	}
	s.pluginAPI.LogInfo("Daily delivery completed",
		"runDate", runDate, "targetDate", targetDate,
		"subscribers", len(subs),
		"sent", sentCount, "nodata", nodataCount, "failed", failCount,
		"testMode", testMode)
}

// daysBetween 은 두 YYYY-MM-DD 날짜 사이의 일수 차이 (later - earlier) 를 반환합니다.
// later 가 earlier 보다 빠르면 음수. 파싱 실패 시 (0, false).
func daysBetween(earlier, later string) (int, bool) {
	a, err := time.ParseInLocation("2006-01-02", earlier, seoulLocation)
	if err != nil {
		return 0, false
	}
	b, err := time.ParseInLocation("2006-01-02", later, seoulLocation)
	if err != nil {
		return 0, false
	}
	diff := b.Sub(a).Hours() / 24
	return int(diff), true
}

// deliverToUser 는 사용자 1명에게 (Summary + 영역별 anomaly) 를 1통의 DM 으로 발송합니다.
//
//   · 모든 영역 리포트의 anomalies 를 평탄화 → 3개 표 (호출수/증가율/Latency)
//   · 메시지 본문은 bot.RenderUserMessage 가 조립
//   · delivery_log 는 (Summary + 매칭 영역) 리포트당 1행씩 기록
//
// 반환: 발송 성공 시 true. 한 사용자 당 1통이므로 boolean 으로 충분.
//
// displayDate 는 메시지 본문 {date} 치환에 사용되는 날짜 (= MAX(reportdate) 기반 발송 대상일).
// log 의 sentat 등 시각 컬럼은 DB 가 NOW() 로 직접 기록.
func (s *DeliveryScheduler) deliverToUser(sub *Subscription, summary *Report, matched []*Report, displayDate string) bool {
	// 1) anomaly 평탄화
	var anomalies []bot.Anomaly
	for _, r := range matched {
		as, err := parseAnomaliesFromRawData(r.RawData)
		if err != nil {
			s.pluginAPI.LogWarn("Failed to parse anomalies from rawdata",
				"reportID", r.ID, "group", r.ResourceGroupCode, "error", err.Error())
			continue
		}
		anomalies = append(anomalies, as...)
	}

	// 2) 메시지 본문 조립
	cfg := bot.RenderConfig{}
	if s.getRenderConfig != nil {
		cfg = s.getRenderConfig()
	}
	summaryText := ""
	if summary != nil {
		summaryText = summary.Content
	}
	content := bot.RenderUserMessage(summaryText, anomalies, displayDate, cfg)

	// 3-4) 발송 + 로그 기록 (공통 헬퍼)
	var allReports []*Report
	if summary != nil {
		allReports = append(allReports, summary)
	}
	allReports = append(allReports, matched...)

	return s.sendAndLog(sub, content, allReports, len(anomalies))
}

// sendAndLog 는 다음 4단계를 묶어서 처리합니다:
//   1. 각 리포트에 대해 delivery_log pending 사전 생성
//   2. 봇 DM 발송
//   3. 성공: 모든 로그를 sent 로 mark, 실패: 모든 로그를 failed 로 mark (재시도 큐 진입)
//   4. 결과 로그
//
// anomaliesCount 는 로깅용 메타. 발송 성공 시 true.
func (s *DeliveryScheduler) sendAndLog(sub *Subscription, content string, reports []*Report, anomaliesCount int) bool {
	chPtr := (*string)(nil)
	if sub.ChannelID != "" {
		c := sub.ChannelID
		chPtr = &c
	}

	// 1) delivery_log 사전 기록 (pending)
	logs := make([]*DeliveryLog, 0, len(reports))
	for _, r := range reports {
		log := &DeliveryLog{
			UserID:         sub.UserID,
			SubscriptionID: sub.ID,
			ReportID:       r.ID,
			ChannelID:      chPtr,
			Status:         DeliveryStatusPending,
		}
		if err := s.deliveryLogStore.Create(log); err != nil {
			s.pluginAPI.LogError("Failed to create delivery log",
				"userID", maskUserID(sub.UserID),
				"reportID", r.ID, "error", err.Error())
			continue
		}
		logs = append(logs, log)
	}

	// 2-3) DM 발송 후 결과 mark
	if err := s.bot.SendReportToUser(sub.UserID, sub.ChannelID, content); err != nil {
		for _, log := range logs {
			_ = s.deliveryLogStore.MarkFailed(log.ID, err.Error())
		}
		s.pluginAPI.LogWarn("Delivery failed (will retry)",
			"userID", maskUserID(sub.UserID),
			"reports", len(reports), "error", err.Error())
		return false
	}
	for _, log := range logs {
		_ = s.deliveryLogStore.MarkSent(log.ID)
	}
	s.pluginAPI.LogInfo("Delivery succeeded",
		"userID", maskUserID(sub.UserID),
		"reports", len(reports),
		"anomalies", anomaliesCount)
	return true
}

// parseAnomaliesFromRawData 는 rawdata jsonb 컬럼에서 anomalies 배열을 꺼냅니다.
// rawdata 는 LLM webhook 페이로드 원본이므로 LLMReportRequest 스키마를 따릅니다.
func parseAnomaliesFromRawData(raw []byte) ([]bot.Anomaly, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var req LLMReportRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	return req.Anomalies, nil
}

func sortedKeys(m map[string]*Report) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// indexReports 는 targetDate 의 전체 리포트 슬라이스를 받아
// SUMMARY 행과 영역별 리포트 맵으로 분리합니다.
// deliverAll / processRetries 가 동일 인덱싱을 수행하므로 공통 헬퍼로 묶음.
func indexReports(reports []*Report) (summary *Report, byGroup map[string]*Report) {
	byGroup = make(map[string]*Report, len(reports))
	for _, r := range reports {
		if r.ResourceGroupCode == SummaryResourceGroupCode {
			summary = r
			continue
		}
		byGroup[r.ResourceGroupCode] = r
	}
	return summary, byGroup
}

// matchReportsForUser 는 사용자 구독의 ResourceGroups 와 byGroup 을 비교해
// 매칭되는 영역 리포트들을 반환합니다.
//
//   · sub.ResourceGroups 가 비어있으면 = 전체 수신 (Summary 제외, code 오름차순)
//   · 비어있지 않으면 = 사용자가 선택한 코드 중 byGroup 에 존재하는 것만
func matchReportsForUser(sub *Subscription, byGroup map[string]*Report) []*Report {
	if len(sub.ResourceGroups) == 0 {
		out := make([]*Report, 0, len(byGroup))
		for _, code := range sortedKeys(byGroup) {
			out = append(out, byGroup[code])
		}
		return out
	}
	out := make([]*Report, 0, len(sub.ResourceGroups))
	for _, code := range sub.ResourceGroups {
		if r, ok := byGroup[code]; ok {
			out = append(out, r)
		}
	}
	return out
}

// deliverNoData는 발송 대상일에 매칭되는 리포트가 없는 구독자에게 폴백 메시지를 보냅니다.
// (LLM 미연동 / LLM이 이상치 없음 으로 판단한 케이스 모두 커버)
//
// displayDate 는 메시지 본문 {date} 치환에 사용되는 날짜 (= MAX(reportdate) 기반 발송 대상일).
// 템플릿은 플러그인 설정의 TemplateNoData 를 사용. delivery_log 에는 기록하지 않습니다.
func (s *DeliveryScheduler) deliverNoData(sub *Subscription, displayDate string) bool {
	dtime := s.getDeliveryTime()

	tmpl := ""
	if s.getNoDataTemplate != nil {
		tmpl = s.getNoDataTemplate()
	}
	msg := bot.RenderNoDataMessage(tmpl, displayDate, dtime)

	if err := s.bot.SendReportToUser(sub.UserID, sub.ChannelID, msg); err != nil {
		s.pluginAPI.LogWarn("No-data delivery failed",
			"userID", maskUserID(sub.UserID), "error", err.Error())
		return false
	}
	s.pluginAPI.LogInfo("No-data fallback sent",
		"userID", maskUserID(sub.UserID), "displayDate", displayDate)
	return true
}

// =============================================================================
// 재시도 루프 — 5분 간격으로 failed && retrycount < 3 픽업
// =============================================================================

func (s *DeliveryScheduler) runRetryLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.processRetries()
		case <-s.stopChan:
			return
		}
	}
}

// processRetries — 실패한 사용자에 대해 deliverToUser 를 재시도.
//
// 신규 메시지 모델에서는 사용자 1명 = 1통 (Summary + 영역 리포트 합본) 이므로
// 단건 reportID 기준이 아니라 (userID, today) 기준으로 재발송합니다.
// failed 로그를 사용자별로 그룹핑한 뒤, 그날의 Summary + 매칭 영역 리포트를
// 다시 모아서 deliverToUser 를 호출하는 방식.
func (s *DeliveryScheduler) processRetries() {
	if s.deliveryLogStore == nil || s.reportStore == nil || s.subscriptionStore == nil || s.bot == nil {
		return
	}
	logs, err := s.deliveryLogStore.ListRetryable(maxRetries)
	if err != nil {
		s.pluginAPI.LogError("Failed to list retryable logs", "error", err.Error())
		return
	}
	if len(logs) == 0 {
		return
	}

	// 같은 사용자에 대한 중복 재시도 방지 — userID 만으로 dedup.
	// (실행일 키를 함께 넣었던 옛 구조의 잔재. 같은 실행 사이클 내에서는 runDate
	// 가 단일 값이므로 userID 만으로 충분.)
	seen := make(map[string]bool)
	runDate := time.Now().In(seoulLocation).Format("2006-01-02")

	// 재시도도 발송 본문과 동일하게 MAX(reportdate) 기준으로 동작.
	// max 가 없거나 너무 오래됐으면 재시도 skip — 처음 발송 시 이미 가드에 걸렸을 것.
	targetDate, found, mdErr := s.reportStore.MaxReportDate()
	if mdErr != nil {
		s.pluginAPI.LogWarn("Retry skipped — MAX(reportdate) failed", "error", mdErr.Error())
		return
	}
	if !found {
		s.pluginAPI.LogInfo("Retry skipped — no reports available")
		return
	}
	if ageDays, ok := daysBetween(targetDate, runDate); ok && ageDays > maxReportAgeDays {
		s.pluginAPI.LogInfo("Retry skipped — latest report too old",
			"targetDate", targetDate, "ageDays", ageDays)
		return
	}

	adminCh := s.getAdminChannelID()

	for _, log := range logs {
		if seen[log.UserID] {
			continue
		}
		seen[log.UserID] = true

		// 사용자 구독 재조회
		sub, sErr := s.subscriptionStore.GetByUserID(log.UserID)
		if sErr != nil || sub == nil || !sub.Active {
			continue
		}

		// targetDate (MAX reportdate) 리포트 재로드
		reports, rerr := s.reportStore.ListByDate(targetDate)
		if rerr != nil {
			continue
		}
		summary, byGroup := indexReports(reports)
		matched := matchReportsForUser(sub, byGroup)

		if summary == nil && len(matched) == 0 {
			// 폴백 메시지 재발송
			if !s.deliverNoData(sub, targetDate) {
				if log.RetryCount+1 >= maxRetries && adminCh != "" {
					_ = s.bot.NotifyAdminChannel(adminCh,
						fmt.Sprintf("AI Service Reporter NoData 재발송 실패: user=%s targetDate=%s",
							maskUserID(log.UserID), targetDate))
				}
			}
			continue
		}

		if !s.deliverToUser(sub, summary, matched, targetDate) {
			if log.RetryCount+1 >= maxRetries && adminCh != "" {
				_ = s.bot.NotifyAdminChannel(adminCh,
					fmt.Sprintf("AI Service Reporter 발송 최종 실패: user=%s targetDate=%s",
						maskUserID(log.UserID), targetDate))
			}
			continue
		}
		s.pluginAPI.LogInfo("Delivery retry succeeded",
			"userID", maskUserID(log.UserID), "targetDate", targetDate)
	}
}

// =============================================================================
// 유틸리티
// =============================================================================

func timeUntilNextMinute() time.Duration {
	now := time.Now()
	next := now.Truncate(time.Minute).Add(time.Minute)
	return next.Sub(now)
}

// tryAcquireMutex는 KV Store 에 멱등 락을 시도합니다. 성공시 true.
//
func tryAcquireMutex(api plugin.API, key string, ttlSeconds int64) bool {
	opts := model.PluginKVSetOptions{
		Atomic:          true,
		OldValue:        nil, // key 가 없을 때만 set
		ExpireInSeconds: ttlSeconds,
	}
	set, appErr := api.KVSetWithOptions(key, []byte("1"), opts)
	if appErr != nil {
		api.LogWarn("KV mutex acquire failed", "key", key, "error", appErr.Error())
		return false
	}
	return set
}
