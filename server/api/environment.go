package api

// Environment는 실행 환경(개발/운영)을 나타냅니다.
type Environment int

const (
	Development Environment = iota
	Production
)

func (e Environment) String() string {
	if e == Production {
		return "production"
	}
	return "development"
}
