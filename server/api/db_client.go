package api

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// DBClient는 PostgreSQL 클라이언트입니다.
type DBClient struct {
	db *sql.DB
}

// NewDBClient는 새로운 DB 클라이언트를 생성합니다.
func NewDBClient(config DBConfig) (*DBClient, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		config.Host, config.Port, config.User, config.Password, config.DBName,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("DB 연결 실패: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("DB Ping 실패: %w", err)
	}

	// 풀 설정
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	return &DBClient{db: db}, nil
}

func (c *DBClient) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

func (c *DBClient) GetDB() *sql.DB {
	return c.db
}
