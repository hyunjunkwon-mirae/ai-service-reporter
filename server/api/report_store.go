package api

import (
	"database/sql"
	"fmt"
	"strings"
)

// ReportStore는 LLM 리포트를 저장/조회합니다.
type ReportStore struct {
	db *sql.DB
}

func NewReportStore(client *DBClient) *ReportStore {
	return &ReportStore{db: client.GetDB()}
}

// Upsert는 (reportdate, resourcegroupcode) 키로 UPSERT 합니다.
// 동일 키 중복 수신 시 content / rawdata 를 덮어씁니다.
//
// ⚠️ 구현 주의:
//   DBA 가 만든 reports 테이블에 (reportdate, resourcegroupcode) UNIQUE 제약이 없어서
//   ON CONFLICT 사용 시 PostgreSQL 에러 (no unique constraint matching) 가 납니다.
//   따라서 SELECT-then-INSERT/UPDATE 패턴으로 처리합니다.
//   LLM Webhook 단일 트래픽이라 race condition 영향 미미.
func (s *ReportStore) Upsert(r *Report) error {
	now := NowEpochMs()

	var raw interface{}
	if len(r.RawData) > 0 {
		raw = r.RawData
	} else {
		raw = nil
	}

	// 1) 기존 행 조회
	var existingID string
	err := s.db.QueryRow(`
		SELECT id FROM ai_service_reporter_reports
		WHERE reportdate = $1 AND resourcegroupcode = $2`,
		r.ReportDate, r.ResourceGroupCode,
	).Scan(&existingID)

	if err == sql.ErrNoRows {
		// 2a) 신규 INSERT
		if r.ID == "" {
			r.ID = NewID()
		}
		_, err = s.db.Exec(`
			INSERT INTO ai_service_reporter_reports
				(id, reportdate, resourcegroupcode, windowdays, content, rawdata, createat, updateat)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
			r.ID, r.ReportDate, r.ResourceGroupCode, r.WindowDays,
			r.Content, raw, now,
		)
		if err != nil {
			return fmt.Errorf("리포트 INSERT 실패: %w", err)
		}
		r.CreateAt = now
		r.UpdateAt = now
		return nil
	} else if err != nil {
		return fmt.Errorf("리포트 조회 실패: %w", err)
	}

	// 2b) 기존 행 UPDATE (id 유지)
	r.ID = existingID
	_, err = s.db.Exec(`
		UPDATE ai_service_reporter_reports
		SET content = $1, rawdata = $2, windowdays = $3, updateat = $4
		WHERE id = $5`,
		r.Content, raw, r.WindowDays, now, existingID,
	)
	if err != nil {
		return fmt.Errorf("리포트 UPDATE 실패: %w", err)
	}
	r.UpdateAt = now
	return nil
}

// ListByDate는 특정 날짜의 모든 리포트를 반환합니다.
func (s *ReportStore) ListByDate(reportDate string) ([]*Report, error) {
	// rawdata 까지 함께 가져옴 — 발송 시점에 scheduler 가 anomalies 평탄화를 위해
	// rawdata 를 파싱하므로 SELECT 에서 누락되면 표가 항상 빈다.
	q := `SELECT id, reportdate::text, resourcegroupcode, windowdays, content, rawdata, createat, updateat
	      FROM ai_service_reporter_reports
	      WHERE reportdate = $1`
	rows, err := s.db.Query(q, reportDate)
	if err != nil {
		return nil, fmt.Errorf("리포트 조회 실패: %w", err)
	}
	defer rows.Close()

	var out []*Report
	for rows.Next() {
		r := &Report{}
		// rawdata 는 NULL 가능 → sql.RawBytes 가 아닌 nullable 처리를 위해 []byte 포인터 흉내
		var raw []byte
		if err := rows.Scan(
			&r.ID, &r.ReportDate, &r.ResourceGroupCode, &r.WindowDays,
			&r.Content, &raw, &r.CreateAt, &r.UpdateAt,
		); err != nil {
			return nil, fmt.Errorf("리포트 스캔 실패: %w", err)
		}
		r.RawData = raw
		out = append(out, r)
	}
	return out, nil
}

// MaxReportDate 는 ai_service_reporter_reports 에 적재된 가장 최근 reportdate 를 반환합니다.
//
// SUMMARY 행도 동일 reportdate 로 들어오는 약속이지만, 운영 중 AIOps 가
// 영역 리포트만 보내고 SUMMARY 를 누락하는 케이스가 있을 수 있으므로
// SUMMARY 를 제외한 영역 리포트 기준으로 max 를 산정 (영역 데이터가 없는데
// SUMMARY 만 떠 있는 날짜로 트리거되어 모두에게 폴백이 발송되는 사고 방지).
//
// 반환:
//   · (date, true, nil)   : 정상 조회. date 는 YYYY-MM-DD 형식.
//   · ("", false, nil)    : 영역 리포트가 1건도 없음.
//   · ("", false, err)    : 쿼리 실패.
func (s *ReportStore) MaxReportDate() (string, bool, error) {
	q := `SELECT MAX(reportdate)::text
	      FROM ai_service_reporter_reports
	      WHERE resourcegroupcode <> 'SUMMARY'`
	var d sql.NullString
	if err := s.db.QueryRow(q).Scan(&d); err != nil {
		return "", false, fmt.Errorf("MAX(reportdate) 조회 실패: %w", err)
	}
	if !d.Valid || d.String == "" {
		return "", false, nil
	}
	return d.String, true, nil
}

// ListByDateAndGroups는 reportdate + 그룹 코드 IN 절로 조회합니다.
// groups가 비어있으면 전체 (= ListByDate와 동일).
func (s *ReportStore) ListByDateAndGroups(reportDate string, groups []string) ([]*Report, error) {
	if len(groups) == 0 {
		return s.ListByDate(reportDate)
	}

	placeholders := make([]string, len(groups))
	args := make([]interface{}, 0, len(groups)+1)
	args = append(args, reportDate)
	for i, g := range groups {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, g)
	}
	q := fmt.Sprintf(`
		SELECT id, reportdate::text, resourcegroupcode, windowdays, content, rawdata, createat, updateat
		FROM ai_service_reporter_reports
		WHERE reportdate = $1 AND resourcegroupcode IN (%s)`,
		strings.Join(placeholders, ","),
	)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("리포트 필터 조회 실패: %w", err)
	}
	defer rows.Close()

	var out []*Report
	for rows.Next() {
		r := &Report{}
		var raw []byte
		if err := rows.Scan(
			&r.ID, &r.ReportDate, &r.ResourceGroupCode, &r.WindowDays,
			&r.Content, &raw, &r.CreateAt, &r.UpdateAt,
		); err != nil {
			return nil, err
		}
		r.RawData = raw
		out = append(out, r)
	}
	return out, nil
}
