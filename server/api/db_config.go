package api

// DBConfig는 PostgreSQL 연결 정보입니다.
// (사내 Mattermost PG 서버 — reference plugin 플러그인 db_config.go 와 동일)
type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
}

// 개발 환경 DB
var DevDBConfig = DBConfig{
	Host:     "10.16.113.10",
	Port:     "15432",
	User:     "mrmattermost",
	Password: "devmt#01",
	DBName:   "dmattermostdb",
}

// 운영 환경 DB
var ProdDBConfig = DBConfig{
	Host:     "10.100.112.25",
	Port:     "15432",
	User:     "mrmattermost",
	Password: "mrsmt#01",
	DBName:   "pmattermostdb",
}

// GetDBConfig는 환경별 DB 설정을 반환합니다.
func GetDBConfig(env Environment) DBConfig {
	if env == Production {
		return ProdDBConfig
	}
	return DevDBConfig
}
