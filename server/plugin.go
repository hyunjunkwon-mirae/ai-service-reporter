package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/miraeasset/ai-service-reporter-plugin/server/api"
	"github.com/miraeasset/ai-service-reporter-plugin/server/bot"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

// =============================================================================
// Plugin 구조체
// =============================================================================

type Plugin struct {
	plugin.MattermostPlugin

	client *pluginapi.Client

	api *api.API
	bot *bot.AIServiceReporterBot

	dbClient *api.DBClient

	subscriptionStore  *api.SubscriptionStore
	resourceGroupStore *api.ResourceGroupStore
	reportStore        *api.ReportStore
	deliveryLogStore   *api.DeliveryLogStore

	scheduler *api.DeliveryScheduler

	configurationLock sync.RWMutex
	configuration     *configuration
}

// =============================================================================
// 라이프사이클
// =============================================================================

func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)
	p.API.LogInfo("AI Service Reporter — OnActivate started")

	// 1) 봇 초기화
	bundlePath, err := p.API.GetBundlePath()
	if err != nil {
		p.API.LogError("Failed to get bundle path", "error", err.Error())
	}
	aiReporterBot, err := bot.New(p.API, bundlePath)
	if err != nil {
		p.API.LogError("Failed to create AI Service Reporter bot", "error", err.Error())
	} else {
		p.bot = aiReporterBot
		p.API.LogInfo("AI Service Reporter bot ready", "botUserID", aiReporterBot.GetBotUserID())
	}

	// 2) DB 초기화 (마이그레이션은 설정에 따라 선택)
	if err := p.initializeDB(); err != nil {
		p.API.LogError("Failed to initialize DB", "error", err.Error())
	}

	// 2.1) 리소스그룹 부트스트랩 — 마스터 테이블에 기본 코드 채움 (idempotent)
	//      DBA 가 테이블만 만들어두면 첫 활성화 시 45개가 자동으로 들어감.
	//      admin UI 의 변경분은 절대 덮어쓰지 않음 (ON CONFLICT DO NOTHING).
	if p.dbClient != nil {
		api.TryBootstrapResourceGroups(p.dbClient.GetDB(), p.API)
	}

	// 3) API 라우터 초기화
	if err := p.initializeAPI(); err != nil {
		return err
	}

	// 4) 스케줄러 시작 — 의존성 부족 시 어느 게 부족한지 명확히 로그
	missing := []string{}
	if p.bot == nil {
		missing = append(missing, "bot")
	}
	if p.subscriptionStore == nil {
		missing = append(missing, "subscriptionStore")
	}
	if p.reportStore == nil {
		missing = append(missing, "reportStore")
	}
	if p.deliveryLogStore == nil {
		missing = append(missing, "deliveryLogStore")
	}

	if len(missing) == 0 {
		cfg := p.getConfiguration()
		p.scheduler = api.NewDeliveryScheduler(
			p.API,
			p.bot,
			p.subscriptionStore,
			p.reportStore,
			p.deliveryLogStore,
			cfg.DeliveryTime,
			cfg.AdminChannelId,
			cfg.TestMode5MinInterval,
			p.noDataTemplateGetter(),
			p.renderConfigGetter(),
		)
		p.scheduler.Start()
	} else {
		p.API.LogWarn("Scheduler not started — missing dependencies",
			"missing", missing)
	}

	p.API.LogInfo("AI Service Reporter plugin activated")
	return nil
}

func (p *Plugin) OnDeactivate() error {
	if p.scheduler != nil {
		p.scheduler.Stop()
	}
	if p.dbClient != nil {
		if err := p.dbClient.Close(); err != nil {
			p.API.LogError("Failed to close DB connection", "error", err.Error())
		}
	}
	p.API.LogInfo("AI Service Reporter plugin deactivated")
	return nil
}

// =============================================================================
// DB 초기화 — 마이그레이션은 AutoMigrateDB 설정 따라 선택
// =============================================================================

func (p *Plugin) initializeDB() error {
	cfg := p.getConfiguration()
	env := p.resolveEnvironment(cfg.Environment)
	dbCfg := api.GetDBConfig(env)

	dbClient, err := api.NewDBClient(dbCfg)
	if err != nil {
		return err
	}
	p.dbClient = dbClient

	if cfg.AutoMigrateDB {
		if err := api.RunMigrations(dbClient.GetDB(), p.API); err != nil {
			return err
		}
		p.API.LogInfo("DB auto-migration executed (AutoMigrateDB=true)")
	} else {
		p.API.LogInfo("DB auto-migration skipped — DBA must run DDL manually (AutoMigrateDB=false)")
	}

	p.subscriptionStore = api.NewSubscriptionStore(dbClient)
	p.resourceGroupStore = api.NewResourceGroupStore(dbClient)
	p.reportStore = api.NewReportStore(dbClient)
	p.deliveryLogStore = api.NewDeliveryLogStore(dbClient)

	p.API.LogInfo("DB initialized", "environment", cfg.Environment)
	return nil
}

// =============================================================================
// API 초기화
// =============================================================================

func (p *Plugin) initializeAPI() error {
	cfg := p.getConfiguration()

	deps := api.Dependencies{
		PluginAPI:          p.API,
		Client:             p.client,
		Bot:                p.bot,
		SubscriptionStore:  p.subscriptionStore,
		ResourceGroupStore: p.resourceGroupStore,
		ReportStore:        p.reportStore,
		DeliveryLogStore:   p.deliveryLogStore,
		WebhookSecret:      cfg.LLMWebhookSecret,
		DB:                 p.dbClientDB(), // 🚨 TEST-ONLY (testdata_routes.go)
		GetDeliveryTime:    p.deliveryTimeGetter(),
		GetVisibleUsers:    p.visibleUsersGetter(),
		GetRenderConfig:    p.renderConfigGetter(),
	}
	p.api = api.NewAPI(deps)
	return nil
}

// dbClientDB 는 dbClient 의 내부 *sql.DB 를 반환합니다. nil 안전.
func (p *Plugin) dbClientDB() *sql.DB {
	if p.dbClient == nil {
		return nil
	}
	return p.dbClient.GetDB()
}

// 매 호출마다 최신 config 값을 반환하는 closure 들 — 설정 변경 즉시 반영
func (p *Plugin) deliveryTimeGetter() func() string {
	return func() string { return p.getConfiguration().DeliveryTime }
}

func (p *Plugin) visibleUsersGetter() func() string {
	return func() string { return p.getConfiguration().VisibleToUsers }
}

func (p *Plugin) noDataTemplateGetter() func() string {
	return func() string { return p.getConfiguration().TemplateNoData }
}

func (p *Plugin) renderConfigGetter() func() bot.RenderConfig {
	return func() bot.RenderConfig {
		c := p.getConfiguration()
		topN := 0
		if c.TableTopN != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(c.TableTopN)); err == nil {
				if n > 0 && n <= 20 {
					topN = n
				}
			}
		}
		return bot.RenderConfig{
			HeaderTemplate:     c.TemplateHeader,
			TableHeaderCall:    c.TableHeaderCallCount,
			TableHeaderCallPct: c.TableHeaderCallCountPct,
			TableHeaderLatency: c.TableHeaderLatency,
			TableNoDataMessage: c.TableNoDataMessage,
			TableTopN:          topN, // 0 이면 templates.go 가 기본값(5) 사용
		}
	}
}

func (p *Plugin) refreshAPI() {
	cfg := p.getConfiguration()

	if p.dbClient != nil {
		_ = p.dbClient.Close()
	}
	if err := p.initializeDB(); err != nil {
		p.API.LogError("Failed to re-init DB on config change", "error", err.Error())
		return
	}

	deps := api.Dependencies{
		PluginAPI:          p.API,
		Client:             p.client,
		Bot:                p.bot,
		SubscriptionStore:  p.subscriptionStore,
		ResourceGroupStore: p.resourceGroupStore,
		ReportStore:        p.reportStore,
		DeliveryLogStore:   p.deliveryLogStore,
		WebhookSecret:      cfg.LLMWebhookSecret,
		DB:                 p.dbClientDB(), // 🚨 TEST-ONLY (testdata_routes.go)
		GetDeliveryTime:    p.deliveryTimeGetter(),
		GetVisibleUsers:    p.visibleUsersGetter(),
		GetRenderConfig:    p.renderConfigGetter(),
	}
	p.api = api.NewAPI(deps)

	if p.scheduler != nil {
		p.scheduler.UpdateStores(p.subscriptionStore, p.reportStore, p.deliveryLogStore)
	}

	p.API.LogInfo("AI Service Reporter API refreshed", "environment", cfg.Environment)
}

func (p *Plugin) resolveEnvironment(envStr string) api.Environment {
	if envStr == "production" {
		return api.Production
	}
	return api.Development
}

// =============================================================================
// HTTP 라우팅
// =============================================================================

func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	if p.api == nil {
		http.Error(w, "API not initialized", http.StatusInternalServerError)
		return
	}
	p.api.GetRouter().ServeHTTP(w, r)
}
