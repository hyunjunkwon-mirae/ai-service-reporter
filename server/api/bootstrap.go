package api

import (
	"database/sql"

	"github.com/mattermost/mattermost/server/public/plugin"
)

// =============================================================================
// 리소스그룹 부트스트랩 시드
//
// 운영 정책:
//   · 활성화 시점에 ai_service_reporter_resourcegroups 테이블에 idempotent 삽입.
//   · 이미 존재하는 code 는 그대로 둠 (ON CONFLICT DO NOTHING) — admin UI 변경 보존.
//   · 테이블이 없으면 silent skip (DBA 가 DDL 실행 전일 수 있음).
//
// 컬럼 매핑 (DBA 제공 스키마 기준):
//   code, name, definition (nullable), sortorder, active, createat, updateat
// =============================================================================

var resourceGroupBootstrapSeeds = []struct {
	Code      string
	Name      string
	SortOrder int
}{
	{"AC", "AC", 10}, {"AD", "AD", 20}, {"AG", "AG", 30}, {"AP", "AP", 40},
	{"AT", "AT", 50}, {"BC", "BC", 60}, {"BK", "BK", 70}, {"BL", "BL", 80},
	{"BM", "BM", 90}, {"BO", "BO", 100}, {"BR", "BR", 110}, {"CB", "CB", 120},
	{"CS", "CS", 130}, {"DC", "DC", 140}, {"DP", "DP", 150}, {"ES", "ES", 160},
	{"FA", "FA", 170}, {"FD", "FD", 180}, {"FF", "FF", 190}, {"FR", "FR", 200},
	{"FS", "FS", 210}, {"FW", "FW", 220}, {"FX", "FX", 230}, {"HM", "HM", 240},
	{"IT", "IT", 250}, {"LN", "LN", 260}, {"MA", "MA", 270}, {"MD", "MD", 280},
	{"MP", "MP", 290}, {"OD", "OD", 300}, {"OT", "OT", 310}, {"PB", "PB", 320},
	{"PD", "PD", 330}, {"PM", "PM", 340}, {"PR", "PR", 350}, {"RA", "RA", 360},
	{"RG", "RG", 370}, {"RK", "RK", 380}, {"SA", "SA", 390}, {"SM", "SM", 400},
	{"ST", "ST", 410}, {"TG", "TG", 420}, {"TS", "TS", 430}, {"UW", "UW", 440},
	{"WA", "WA", 450},
}

func TryBootstrapResourceGroups(db *sql.DB, api plugin.API) {
	if db == nil {
		api.LogWarn("Resource group bootstrap skipped — DB not initialized")
		return
	}

	q := `INSERT INTO ai_service_reporter_resourcegroups
	        (code, name, definition, sortorder, active, createat, updateat)
	      VALUES ($1, $2, NULL, $3, true, $4, $4)
	      ON CONFLICT (code) DO NOTHING`
	now := NowEpochMs()

	inserted := 0
	for _, r := range resourceGroupBootstrapSeeds {
		res, err := db.Exec(q, r.Code, r.Name, r.SortOrder, now)
		if err != nil {
			api.LogWarn("Resource group bootstrap skipped",
				"reason", err.Error(),
				"hint", "DBA 가 ai_service_reporter_resourcegroups 테이블을 만들었는지 확인하세요.")
			return
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	if inserted > 0 {
		api.LogInfo("Resource group bootstrap inserted new codes",
			"inserted", inserted, "total", len(resourceGroupBootstrapSeeds))
	} else {
		api.LogInfo("Resource group bootstrap — no new codes (all exist)",
			"total", len(resourceGroupBootstrapSeeds))
	}
}
