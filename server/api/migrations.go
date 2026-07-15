package api

import (
	"database/sql"
	"fmt"

	"github.com/mattermost/mattermost/server/public/plugin"
)

// =============================================================================
// 스키마 마이그레이션
//
// ⚠️ 운영 정책:
//   · DB 테이블 DROP/CREATE 는 DBA 가 수동 실행.
//   · plugin 은 DDL 을 발급하지 않음 — 이 슬라이스는 비어 있음.
//   · 향후 마이그레이션 필요시 SQL 추가하고 AutoMigrateDB=true 로 적용.
// =============================================================================

// 마이그레이션 SQL — DBA 수동 실행 정책으로 비워둠.
var migrations = []string{
	// 향후 DDL 변경이 plugin 책임이 되면 여기에 추가.
}

// RunMigrations 는 마이그레이션을 적용합니다.
// 현재는 슬라이스가 비어있어 실질적으로 noop 입니다.
func RunMigrations(db *sql.DB, api plugin.API) error {
	for i, stmt := range migrations {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migration #%d 실패: %w", i+1, err)
		}
	}
	api.LogInfo("AI Service Reporter migrations applied", "count", len(migrations))
	return nil
}
