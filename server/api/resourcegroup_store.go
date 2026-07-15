package api

import (
	"database/sql"
	"fmt"
	"strings"
)

// ResourceGroupStore는 ai_service_reporter_resourcegroups 테이블 CRUD 를 담당합니다.
// admin 이 RHS 관리자 탭에서 추가/수정/삭제하면 이 store 를 통해 DB 에 반영됩니다.
type ResourceGroupStore struct {
	db *sql.DB
}

func NewResourceGroupStore(client *DBClient) *ResourceGroupStore {
	return &ResourceGroupStore{db: client.GetDB()}
}

// =============================================================================
// 조회
// =============================================================================

// ListActive 는 active=true 코드만 sortorder 순으로 반환합니다. (사용자/AIOps API 용)
func (s *ResourceGroupStore) ListActive() ([]*ResourceGroup, error) {
	q := `SELECT code, name, definition, sortorder, active
	      FROM ai_service_reporter_resourcegroups
	      WHERE active = true
	      ORDER BY sortorder ASC, code ASC`
	return s.queryGroups(q)
}

// ListAll 은 비활성 포함 전체 코드를 반환합니다. (admin UI 용)
func (s *ResourceGroupStore) ListAll() ([]*ResourceGroup, error) {
	q := `SELECT code, name, definition, sortorder, active
	      FROM ai_service_reporter_resourcegroups
	      ORDER BY sortorder ASC, code ASC`
	return s.queryGroups(q)
}

// ListActiveCodes 는 active=true 코드 문자열 배열을 반환합니다. (AIOps API 응답용)
func (s *ResourceGroupStore) ListActiveCodes() ([]string, error) {
	groups, err := s.ListActive()
	if err != nil {
		return nil, err
	}
	codes := make([]string, len(groups))
	for i, g := range groups {
		codes[i] = g.Code
	}
	return codes, nil
}

// AllValidCodes 는 active=true 코드 집합을 반환합니다. (검증용)
func (s *ResourceGroupStore) AllValidCodes() (map[string]struct{}, error) {
	groups, err := s.ListActive()
	if err != nil {
		return nil, err
	}
	codes := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		codes[g.Code] = struct{}{}
	}
	return codes, nil
}

// GetByCode 는 단건 조회 (admin UI 의 수정 시 사용).
func (s *ResourceGroupStore) GetByCode(code string) (*ResourceGroup, error) {
	q := `SELECT code, name, definition, sortorder, active
	      FROM ai_service_reporter_resourcegroups
	      WHERE code = $1`
	row := s.db.QueryRow(q, code)
	rg := &ResourceGroup{}
	var (
		name      sql.NullString
		defn      sql.NullString
		sortorder sql.NullInt32
		active    sql.NullBool
	)
	if err := row.Scan(&rg.Code, &name, &defn, &sortorder, &active); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("리소스그룹 조회 실패: %w", err)
	}
	if name.Valid {
		rg.Name = name.String
	} else {
		rg.Name = rg.Code
	}
	if defn.Valid {
		d := defn.String
		rg.Definition = &d
	}
	if sortorder.Valid {
		rg.SortOrder = int(sortorder.Int32)
	}
	if active.Valid {
		rg.Active = active.Bool
	}
	return rg, nil
}

// =============================================================================
// 변경
// =============================================================================

// Create 는 새 코드를 추가합니다. 이미 존재하면 에러.
func (s *ResourceGroupStore) Create(rg *ResourceGroup) error {
	rg.Code = strings.TrimSpace(strings.ToUpper(rg.Code))
	if rg.Code == "" {
		return fmt.Errorf("code 는 필수입니다.")
	}
	// SUMMARY 는 일 단위 글로벌 Summary 전용 sentinel 코드.
	// 마스터에 들어가면 사용자 RHS 칩에 노출되므로 차단.
	if rg.Code == SummaryResourceGroupCode {
		return fmt.Errorf("%s 는 예약된 코드이므로 마스터에 추가할 수 없습니다.", SummaryResourceGroupCode)
	}
	if rg.Name == "" {
		rg.Name = rg.Code
	}

	now := NowEpochMs()
	var desc interface{}
	if rg.Definition != nil && *rg.Definition != "" {
		desc = *rg.Definition
	}

	_, err := s.db.Exec(`
		INSERT INTO ai_service_reporter_resourcegroups
			(code, name, definition, sortorder, active, createat, updateat)
		VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		rg.Code, rg.Name, desc, rg.SortOrder, rg.Active, now,
	)
	if err != nil {
		return fmt.Errorf("리소스그룹 생성 실패: %w", err)
	}
	return nil
}

// Update 는 기존 코드의 메타데이터(name/definition/sortorder/active) 를 수정합니다.
// code 자체는 PK 이므로 변경 불가 (변경하려면 삭제 후 재생성).
func (s *ResourceGroupStore) Update(rg *ResourceGroup) error {
	rg.Code = strings.TrimSpace(strings.ToUpper(rg.Code))
	if rg.Code == "" {
		return fmt.Errorf("code 는 필수입니다.")
	}

	now := NowEpochMs()
	var desc interface{}
	if rg.Definition != nil && *rg.Definition != "" {
		desc = *rg.Definition
	}

	res, err := s.db.Exec(`
		UPDATE ai_service_reporter_resourcegroups
		SET name = $2, definition = $3, sortorder = $4, active = $5, updateat = $6
		WHERE code = $1`,
		rg.Code, rg.Name, desc, rg.SortOrder, rg.Active, now,
	)
	if err != nil {
		return fmt.Errorf("리소스그룹 수정 실패: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("code 를 찾을 수 없습니다: %s", rg.Code)
	}
	return nil
}

// Delete 는 코드를 영구 삭제합니다.
// ⚠️ 외래키 (구독 매핑 등) 가 있을 경우 DB FK 에 따라 cascade 또는 reject 됨.
//   안전한 대안은 Update 로 active=false 처리.
func (s *ResourceGroupStore) Delete(code string) error {
	code = strings.TrimSpace(strings.ToUpper(code))
	if code == "" {
		return fmt.Errorf("code 는 필수입니다.")
	}
	res, err := s.db.Exec(`DELETE FROM ai_service_reporter_resourcegroups WHERE code = $1`, code)
	if err != nil {
		return fmt.Errorf("리소스그룹 삭제 실패: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("code 를 찾을 수 없습니다: %s", code)
	}
	return nil
}

// =============================================================================
// 내부 헬퍼
// =============================================================================

func (s *ResourceGroupStore) queryGroups(query string, args ...interface{}) ([]*ResourceGroup, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("리소스그룹 조회 실패: %w", err)
	}
	defer rows.Close()

	var out []*ResourceGroup
	for rows.Next() {
		rg := &ResourceGroup{}
		var (
			name      sql.NullString
			defn      sql.NullString
			sortorder sql.NullInt32
			active    sql.NullBool
		)
		if err := rows.Scan(&rg.Code, &name, &defn, &sortorder, &active); err != nil {
			return nil, fmt.Errorf("리소스그룹 스캔 실패: %w", err)
		}
		if name.Valid {
			rg.Name = name.String
		} else {
			rg.Name = rg.Code
		}
		if defn.Valid {
			d := defn.String
			rg.Definition = &d
		}
		if sortorder.Valid {
			rg.SortOrder = int(sortorder.Int32)
		}
		if active.Valid {
			rg.Active = active.Bool
		}
		out = append(out, rg)
	}
	return out, nil
}
