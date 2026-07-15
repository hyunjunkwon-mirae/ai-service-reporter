package api

import (
	"database/sql"
	"fmt"
)

// SubscriptionStore는 구독 CRUD를 처리합니다.
type SubscriptionStore struct {
	db *sql.DB
}

func NewSubscriptionStore(client *DBClient) *SubscriptionStore {
	return &SubscriptionStore{db: client.GetDB()}
}

// =============================================================================
// 조회
// =============================================================================

// GetByUserID는 사용자 ID로 활성 구독을 조회합니다. 없으면 (nil, nil).
func (s *SubscriptionStore) GetByUserID(userID string) (*Subscription, error) {
	q := `SELECT id, userid, active, COALESCE(channelid, ''), createat, updateat, deleteat
	      FROM ai_service_reporter_subscriptions
	      WHERE userid = $1 AND deleteat = 0
	      LIMIT 1`

	sub := &Subscription{}
	err := s.db.QueryRow(q, userID).Scan(
		&sub.ID, &sub.UserID, &sub.Active, &sub.ChannelID,
		&sub.CreateAt, &sub.UpdateAt, &sub.DeleteAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("구독 조회 실패: %w", err)
	}

	groups, err := s.getResourceGroups(sub.ID)
	if err != nil {
		return nil, err
	}
	sub.ResourceGroups = groups
	return sub, nil
}

// ListActive는 발송 대상이 되는 활성 구독을 모두 조회합니다.
func (s *SubscriptionStore) ListActive() ([]*Subscription, error) {
	q := `SELECT id, userid, active, COALESCE(channelid, ''), createat, updateat, deleteat
	      FROM ai_service_reporter_subscriptions
	      WHERE active = true AND deleteat = 0`

	rows, err := s.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("활성 구독 목록 조회 실패: %w", err)
	}
	defer rows.Close()

	var subs []*Subscription
	for rows.Next() {
		sub := &Subscription{}
		if err := rows.Scan(
			&sub.ID, &sub.UserID, &sub.Active, &sub.ChannelID,
			&sub.CreateAt, &sub.UpdateAt, &sub.DeleteAt,
		); err != nil {
			return nil, fmt.Errorf("구독 스캔 실패: %w", err)
		}
		subs = append(subs, sub)
	}

	// 각 구독의 그룹 로드
	for _, sub := range subs {
		groups, err := s.getResourceGroups(sub.ID)
		if err != nil {
			return nil, err
		}
		sub.ResourceGroups = groups
	}
	return subs, nil
}

// getResourceGroups는 특정 구독의 리소스그룹 코드 목록을 반환합니다.
func (s *SubscriptionStore) getResourceGroups(subscriptionID string) ([]string, error) {
	q := `SELECT resourcegroupcode FROM ai_service_reporter_subscription_groups
	      WHERE subscriptionid = $1 ORDER BY resourcegroupcode`

	rows, err := s.db.Query(q, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("리소스그룹 매핑 조회 실패: %w", err)
	}
	defer rows.Close()

	codes := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, nil
}

// =============================================================================
// 생성 / 수정 (Upsert)
// =============================================================================

// Upsert는 구독을 생성하거나 갱신합니다. (UserID 기준)
// 리소스그룹은 매핑 테이블을 통째로 교체합니다.
func (s *SubscriptionStore) Upsert(sub *Subscription) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("트랜잭션 시작 실패: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := NowEpochMs()

	// 1) 기존 활성 구독 조회
	var existingID string
	err = tx.QueryRow(`SELECT id FROM ai_service_reporter_subscriptions WHERE userid = $1 AND deleteat = 0 LIMIT 1`,
		sub.UserID).Scan(&existingID)

	if err == sql.ErrNoRows {
		// 신규 생성
		sub.ID = NewID()
		sub.CreateAt = now
		sub.UpdateAt = now
		sub.DeleteAt = 0

		channelID := sql.NullString{String: sub.ChannelID, Valid: sub.ChannelID != ""}
		if _, err = tx.Exec(`
			INSERT INTO ai_service_reporter_subscriptions (id, userid, active, channelid, createat, updateat, deleteat)
			VALUES ($1, $2, $3, $4, $5, $5, 0)`,
			sub.ID, sub.UserID, sub.Active, channelID, now,
		); err != nil {
			return fmt.Errorf("구독 INSERT 실패: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("기존 구독 확인 실패: %w", err)
	} else {
		// 갱신
		sub.ID = existingID
		sub.UpdateAt = now

		channelID := sql.NullString{String: sub.ChannelID, Valid: sub.ChannelID != ""}
		if _, err = tx.Exec(`
			UPDATE ai_service_reporter_subscriptions
			SET active = $2, channelid = $3, updateat = $4
			WHERE id = $1`,
			sub.ID, sub.Active, channelID, now,
		); err != nil {
			return fmt.Errorf("구독 UPDATE 실패: %w", err)
		}
	}

	// 2) 그룹 매핑 재구성 (delete-all + insert)
	if _, err = tx.Exec(`DELETE FROM ai_service_reporter_subscription_groups WHERE subscriptionid = $1`, sub.ID); err != nil {
		return fmt.Errorf("기존 그룹 삭제 실패: %w", err)
	}

	if len(sub.ResourceGroups) > 0 {
		stmt, perr := tx.Prepare(`
			INSERT INTO ai_service_reporter_subscription_groups (subscriptionid, resourcegroupcode, createat)
			VALUES ($1, $2, $3)`)
		if perr != nil {
			err = fmt.Errorf("그룹 prepare 실패: %w", perr)
			return err
		}
		defer stmt.Close()

		for _, code := range sub.ResourceGroups {
			if _, ierr := stmt.Exec(sub.ID, code, now); ierr != nil {
				err = fmt.Errorf("그룹 INSERT 실패(%s): %w", code, ierr)
				return err
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("커밋 실패: %w", err)
	}
	return nil
}

// =============================================================================
// 삭제 (soft delete)
// =============================================================================

// SoftDeleteByUserID는 사용자의 구독을 비활성화 + soft delete 처리합니다.
func (s *SubscriptionStore) SoftDeleteByUserID(userID string) error {
	now := NowEpochMs()
	_, err := s.db.Exec(`
		UPDATE ai_service_reporter_subscriptions
		SET active = false, deleteat = $2, updateat = $2
		WHERE userid = $1 AND deleteat = 0`,
		userID, now,
	)
	if err != nil {
		return fmt.Errorf("구독 soft delete 실패: %w", err)
	}
	return nil
}
