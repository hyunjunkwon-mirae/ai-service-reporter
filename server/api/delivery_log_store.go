package api

import (
	"database/sql"
	"fmt"
)

// DeliveryLogStore는 발송 이력을 관리합니다.
type DeliveryLogStore struct {
	db *sql.DB
}

func NewDeliveryLogStore(client *DBClient) *DeliveryLogStore {
	return &DeliveryLogStore{db: client.GetDB()}
}

// Create는 신규 로그를 생성합니다. (status=pending)
func (s *DeliveryLogStore) Create(log *DeliveryLog) error {
	if log.ID == "" {
		log.ID = NewID()
	}
	now := NowEpochMs()
	log.CreateAt = now
	log.UpdateAt = now

	var channelID sql.NullString
	if log.ChannelID != nil && *log.ChannelID != "" {
		channelID = sql.NullString{String: *log.ChannelID, Valid: true}
	}

	_, err := s.db.Exec(`
		INSERT INTO ai_service_reporter_delivery_log
			(id, userid, subscriptionid, reportid, channelid, status, retrycount, createat, updateat)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
		log.ID, log.UserID, log.SubscriptionID, log.ReportID, channelID,
		log.Status, log.RetryCount, now,
	)
	if err != nil {
		return fmt.Errorf("발송 로그 생성 실패: %w", err)
	}
	return nil
}

// MarkSent는 발송 성공으로 상태 변경합니다.
func (s *DeliveryLogStore) MarkSent(id string) error {
	now := NowEpochMs()
	_, err := s.db.Exec(`
		UPDATE ai_service_reporter_delivery_log
		SET status = $1, sentat = $2, updateat = $2, error = NULL
		WHERE id = $3`,
		DeliveryStatusSent, now, id,
	)
	if err != nil {
		return fmt.Errorf("발송 성공 기록 실패: %w", err)
	}
	return nil
}

// MarkFailed는 실패로 기록합니다. (retrycount 증가)
func (s *DeliveryLogStore) MarkFailed(id, errMsg string) error {
	now := NowEpochMs()
	_, err := s.db.Exec(`
		UPDATE ai_service_reporter_delivery_log
		SET status = $1, error = $2, retrycount = retrycount + 1, updateat = $3
		WHERE id = $4`,
		DeliveryStatusFailed, errMsg, now, id,
	)
	if err != nil {
		return fmt.Errorf("발송 실패 기록 실패: %w", err)
	}
	return nil
}

// ListRetryable는 retrycount < maxRetries 이고 status = failed 인 로그를 조회합니다.
func (s *DeliveryLogStore) ListRetryable(maxRetries int) ([]*DeliveryLog, error) {
	q := `SELECT id, userid, subscriptionid, reportid, channelid, status, retrycount, error, sentat, createat, updateat
	      FROM ai_service_reporter_delivery_log
	      WHERE status = $1 AND retrycount < $2
	      ORDER BY createat ASC`
	rows, err := s.db.Query(q, DeliveryStatusFailed, maxRetries)
	if err != nil {
		return nil, fmt.Errorf("재시도 대상 조회 실패: %w", err)
	}
	defer rows.Close()
	return scanDeliveryLogs(rows)
}

// ListByUser는 사용자별 최근 발송 이력을 반환합니다.
func (s *DeliveryLogStore) ListByUser(userID string, limit int) ([]*DeliveryLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, userid, subscriptionid, reportid, channelid, status, retrycount, error, sentat, createat, updateat
	      FROM ai_service_reporter_delivery_log
	      WHERE userid = $1
	      ORDER BY createat DESC
	      LIMIT $2`
	rows, err := s.db.Query(q, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("발송 이력 조회 실패: %w", err)
	}
	defer rows.Close()
	return scanDeliveryLogs(rows)
}

// ExistsForToday는 이미 같은 (userID, reportID) 조합으로 sent 가 있는지 확인합니다.
// (스케줄러 멱등성용)
func (s *DeliveryLogStore) ExistsForReport(userID, reportID string) (bool, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM ai_service_reporter_delivery_log
		WHERE userid = $1 AND reportid = $2 AND status = $3`,
		userID, reportID, DeliveryStatusSent,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func scanDeliveryLogs(rows *sql.Rows) ([]*DeliveryLog, error) {
	var out []*DeliveryLog
	for rows.Next() {
		l := &DeliveryLog{}
		var channelID sql.NullString
		var errMsg sql.NullString
		var sentAt sql.NullInt64

		if err := rows.Scan(
			&l.ID, &l.UserID, &l.SubscriptionID, &l.ReportID,
			&channelID, &l.Status, &l.RetryCount, &errMsg, &sentAt,
			&l.CreateAt, &l.UpdateAt,
		); err != nil {
			return nil, fmt.Errorf("발송 로그 스캔 실패: %w", err)
		}
		if channelID.Valid {
			c := channelID.String
			l.ChannelID = &c
		}
		if errMsg.Valid {
			e := errMsg.String
			l.Error = &e
		}
		if sentAt.Valid {
			t := sentAt.Int64
			l.SentAt = &t
		}
		out = append(out, l)
	}
	return out, nil
}
