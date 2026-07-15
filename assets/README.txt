이 디렉토리에는 봇 프로필 아이콘 (ai_service_reporter.png, 128x128 권장) 을
운영 배포 전에 추가해 주세요.

서버 측 bot.go 의 setBotIcon() 이 OnActivate 시 이 파일을 읽어
봇 사용자 프로필에 등록합니다.

· 없어도 플러그인 동작에는 영향 없음 (LogWarn만 남김)
· dmove-ews 플러그인의 calendar.png 와 동일한 위치/역할
