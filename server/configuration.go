package main

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/pkg/errors"
)

// configuration captures the plugin's external configuration as exposed in the
// Mattermost server configuration. Public fields are deserialized from server
// configuration in OnConfigurationChange.
type configuration struct {
	Environment          string `json:"Environment"`
	AutoMigrateDB        bool   `json:"AutoMigrateDB"`
	LLMWebhookSecret     string `json:"LLMWebhookSecret"`
	DeliveryTime         string `json:"DeliveryTime"`         // "HH:MM" KST
	AdminChannelId       string `json:"AdminChannelId"`
	TestMode5MinInterval bool   `json:"TestMode5MinInterval"`

	// 표출 권한 — 콤마구분 사번 목록. "*" 또는 빈 값이면 모두.
	VisibleToUsers string `json:"VisibleToUsers"`

	// 메시지 헤더 / 폴백 — \n 리터럴을 실제 개행으로 정규화하여 보관.
	TemplateHeader string `json:"TemplateHeader"`
	TemplateNoData string `json:"TemplateNoData"`

	// 표 라벨 / Top N (TR Statistics Summary)
	TableHeaderCallCount    string `json:"TableHeaderCallCount"`
	TableHeaderCallCountPct string `json:"TableHeaderCallCountPct"`
	TableHeaderLatency      string `json:"TableHeaderLatency"`
	TableNoDataMessage      string `json:"TableNoDataMessage"`
	TableTopN               string `json:"TableTopN"` // string 으로 입력받아 파싱
}

// Clone shallow copies the configuration.
func (c *configuration) Clone() *configuration {
	clone := *c
	return &clone
}

// getConfiguration retrieves the active configuration under lock.
func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}
	return p.configuration
}

// setConfiguration replaces the active configuration under lock.
func (p *Plugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return
		}
		panic("setConfiguration called with the existing configuration")
	}
	p.configuration = configuration
}

// timeFormatRegex 는 HH:MM (24시간제) 검증용 정규식입니다.
var timeFormatRegex = regexp.MustCompile(`^([0-1]?\d|2[0-3]):([0-5]\d)$`)

// normalizeDeliveryTime 은 "8:30", " 10:00 " 같은 입력을 "10:00" 같은 정규 형식으로 변환합니다.
// 검증 실패 시 기본값 "10:00" 반환.
func normalizeDeliveryTime(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "10:00", false
	}
	m := timeFormatRegex.FindStringSubmatch(trimmed)
	if m == nil {
		return "10:00", false
	}
	hh := m[1]
	if len(hh) == 1 {
		hh = "0" + hh
	}
	return hh + ":" + m[2], true
}

// normalizeTemplate 은 admin UI 에서 "\n" 리터럴로 입력된 줄바꿈을
// 실제 개행 문자로 변환합니다. 빈 값은 그대로 둡니다 (호출 시점에 기본값 적용).
func normalizeTemplate(s string) string {
	if s == "" {
		return s
	}
	return strings.ReplaceAll(s, "\\n", "\n")
}

// OnConfigurationChange는 설정 변경 시 호출됩니다.
func (p *Plugin) OnConfigurationChange() error {
	newConfig := new(configuration)

	if err := p.API.LoadPluginConfiguration(newConfig); err != nil {
		return errors.Wrap(err, "failed to load plugin configuration")
	}

	// DeliveryTime 검증 + 정규화
	normalized, ok := normalizeDeliveryTime(newConfig.DeliveryTime)
	if !ok && newConfig.DeliveryTime != "" {
		p.API.LogWarn("Invalid DeliveryTime format — fallback to 10:00",
			"input", newConfig.DeliveryTime)
	}
	newConfig.DeliveryTime = normalized

	// 템플릿 줄바꿈 정규화 ("\n" → 실제 개행)
	newConfig.TemplateHeader = normalizeTemplate(newConfig.TemplateHeader)
	newConfig.TemplateNoData = normalizeTemplate(newConfig.TemplateNoData)

	oldConfig := p.getConfiguration()
	environmentChanged := oldConfig.Environment != newConfig.Environment
	deliveryTimeChanged := oldConfig.DeliveryTime != newConfig.DeliveryTime
	testModeChanged := oldConfig.TestMode5MinInterval != newConfig.TestMode5MinInterval
	adminChannelChanged := oldConfig.AdminChannelId != newConfig.AdminChannelId

	p.setConfiguration(newConfig)

	// 환경 변경 시 API 재초기화 (DB 재연결 + 스토어 재생성)
	if environmentChanged && p.api != nil {
		p.refreshAPI()
	}

	// 발송시간/테스트모드/관리채널 중 하나라도 바뀌었으면 스케줄러 재시작
	// (셋 다 같은 ticker 가 사용하는 값이라 한 번에 재시작이 깔끔)
	if (deliveryTimeChanged || testModeChanged || adminChannelChanged) && p.scheduler != nil {
		p.API.LogInfo("Restarting scheduler due to config change",
			"deliveryTime", newConfig.DeliveryTime,
			"testMode", newConfig.TestMode5MinInterval,
			"adminChannelChanged", adminChannelChanged,
			"deliveryTimeChanged", deliveryTimeChanged,
			"testModeChanged", testModeChanged,
		)
		p.scheduler.Restart(
			newConfig.DeliveryTime,
			newConfig.AdminChannelId,
			newConfig.TestMode5MinInterval,
		)
	}

	// 템플릿/표출권한은 closure 로 매번 최신값을 읽으므로 별도 전파 불필요.
	return nil
}
