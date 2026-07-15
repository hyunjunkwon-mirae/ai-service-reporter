package bot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

const (
	BotUsername    = "ai_service_reporter"
	BotDisplayName = "AI Service Reporter"
	BotDescription = "LLM 분석결과를 매일 10:00 발송하는 사내 리포터 봇입니다."
)

// AIServiceReporterBot은 AI Service Reporter 알림 봇을 관리합니다.
// (참고 플러그인의 CalendarBot 과 동일한 패턴)
type AIServiceReporterBot struct {
	pluginAPI  plugin.API
	botUserID  string
	bundlePath string
}

// New는 새로운 AIServiceReporterBot 을 생성합니다.
func New(pluginAPI plugin.API, bundlePath string) (*AIServiceReporterBot, error) {
	b := &AIServiceReporterBot{
		pluginAPI:  pluginAPI,
		bundlePath: bundlePath,
	}
	botUserID, err := b.ensureBot()
	if err != nil {
		return nil, err
	}
	b.botUserID = botUserID
	b.setBotIcon()
	return b, nil
}

// ensureBot은 ai_service_reporter 을 생성하거나 기존 봇을 반환합니다.
func (b *AIServiceReporterBot) ensureBot() (string, error) {
	// 이미 존재하는지 확인
	user, appErr := b.pluginAPI.GetUserByUsername(BotUsername)
	if appErr == nil && user != nil {
		return user.Id, nil
	}
	created, appErr := b.pluginAPI.CreateBot(&model.Bot{
		Username:    BotUsername,
		DisplayName: BotDisplayName,
		Description: BotDescription,
	})
	if appErr != nil {
		return "", fmt.Errorf("봇 생성 실패: %v", appErr)
	}
	return created.UserId, nil
}

// setBotIcon은 봇 프로필 이미지를 설정합니다.
func (b *AIServiceReporterBot) setBotIcon() {
	iconPath := filepath.Join(b.bundlePath, "assets", "ai_service_reporter.png")
	data, err := os.ReadFile(iconPath)
	if err != nil {
		b.pluginAPI.LogWarn("Bot icon not found", "path", iconPath, "error", err.Error())
		return
	}
	if appErr := b.pluginAPI.SetProfileImage(b.botUserID, data); appErr != nil {
		b.pluginAPI.LogWarn("Failed to set bot icon", "error", appErr.Error())
	}
}

// GetBotUserID는 봇 사용자 ID를 반환합니다.
func (b *AIServiceReporterBot) GetBotUserID() string {
	return b.botUserID
}

// =============================================================================
// 발송 API
// =============================================================================

// SendReportToUser는 리포트 본문을 사용자에게 발송합니다.
//   · channelID 가 "" 이면 본인 DM 채널
//   · content 내 "{user.nickname}" 자리표시자를 사용자 닉네임으로 치환
func (b *AIServiceReporterBot) SendReportToUser(userID, channelID, content string) error {
	if b.botUserID == "" {
		return fmt.Errorf("봇이 초기화되지 않았습니다.")
	}

	// 닉네임 치환
	finalMessage := b.replaceNickname(userID, content)

	// 발송 채널 결정
	targetChannel := channelID
	if targetChannel == "" {
		ch, err := b.GetOrCreateDMChannel(userID)
		if err != nil {
			return err
		}
		targetChannel = ch.Id
	}

	post := &model.Post{
		UserId:    b.botUserID,
		ChannelId: targetChannel,
		Message:   finalMessage,
	}
	if _, appErr := b.pluginAPI.CreatePost(post); appErr != nil {
		return fmt.Errorf("리포트 발송 실패: %v", appErr)
	}
	return nil
}

// SendChangeSummary는 구독 변경 후 본인 DM 으로 변경 요약을 보냅니다.
//
// 시그니처는 primitives만 받습니다 — api 패키지의 Subscription 타입과
// 의존 관계를 끊어 import 사이클을 방지합니다.
func (b *AIServiceReporterBot) SendChangeSummary(userID string, active bool, channelID string, resourceGroups []string, deliveryTime string) error {
	if b.botUserID == "" {
		return fmt.Errorf("봇이 초기화되지 않았습니다.")
	}

	ch, err := b.GetOrCreateDMChannel(userID)
	if err != nil {
		return err
	}

	status := "ON"
	if !active {
		status = "OFF"
	}

	channel := "DM"
	if channelID != "" {
		channel = channelID
	}

	groupSummary := "전체"
	if len(resourceGroups) > 0 {
		groupSummary = fmt.Sprintf("%d개 (%s)",
			len(resourceGroups),
			strings.Join(resourceGroups, ", "),
		)
	}

	if deliveryTime == "" {
		deliveryTime = "10:00"
	}

	msg := fmt.Sprintf(`#### 📬 구독 설정이 업데이트됐어요!
· 구독: **%s**
· 리소스그룹: %s
· 발송 채널: %s
· 발송 시간: 매일 %s (KST)

다음 발송부터 적용돼요.`, status, groupSummary, channel, deliveryTime)

	post := &model.Post{
		UserId:    b.botUserID,
		ChannelId: ch.Id,
		Message:   msg,
	}
	if _, appErr := b.pluginAPI.CreatePost(post); appErr != nil {
		return fmt.Errorf("변경 요약 발송 실패: %v", appErr)
	}
	return nil
}

// NotifyAdminChannel은 관리자 채널에 운영 알림을 보냅니다.
func (b *AIServiceReporterBot) NotifyAdminChannel(channelID, message string) error {
	if b.botUserID == "" || channelID == "" {
		return nil
	}
	post := &model.Post{
		UserId:    b.botUserID,
		ChannelId: channelID,
		Message:   "🚨 [AI Service Reporter] " + message,
	}
	if _, appErr := b.pluginAPI.CreatePost(post); appErr != nil {
		return fmt.Errorf("관리자 채널 알림 실패: %v", appErr)
	}
	return nil
}

// =============================================================================
// 도우미
// =============================================================================

// GetOrCreateDMChannel은 봇과 사용자 간 DM 채널을 생성/조회합니다.
func (b *AIServiceReporterBot) GetOrCreateDMChannel(userID string) (*model.Channel, error) {
	ch, appErr := b.pluginAPI.GetDirectChannel(b.botUserID, userID)
	if appErr != nil {
		return nil, fmt.Errorf("DM 채널 생성/조회 실패: %v", appErr)
	}
	return ch, nil
}

// IsBotInChannel은 봇이 해당 채널의 멤버인지 확인합니다.
func (b *AIServiceReporterBot) IsBotInChannel(channelID string) (bool, error) {
	if b.botUserID == "" {
		return false, fmt.Errorf("봇이 초기화되지 않았습니다.")
	}
	_, appErr := b.pluginAPI.GetChannelMember(channelID, b.botUserID)
	if appErr != nil {
		// 404 → 멤버 아님 (에러는 아님)
		if appErr.StatusCode == 404 {
			return false, nil
		}
		return false, fmt.Errorf("채널 멤버 확인 실패: %v", appErr)
	}
	return true, nil
}

// replaceNickname은 content 내 {user.nickname} 자리표시자를 사용자 닉네임으로 치환합니다.
// 닉네임이 없으면 username, 그것도 없으면 "" 사용.
func (b *AIServiceReporterBot) replaceNickname(userID, content string) string {
	if !strings.Contains(content, "{user.nickname}") {
		return content
	}
	nick := ""
	user, appErr := b.pluginAPI.GetUser(userID)
	if appErr == nil && user != nil {
		if user.Nickname != "" {
			nick = user.Nickname
		} else if user.Username != "" {
			nick = user.Username
		}
	}
	return strings.ReplaceAll(content, "{user.nickname}", nick)
}
