# AI Service Reporter — Mattermost Plugin

> 사내 Mattermost 플러그인. LLM이 매일 분석한 업무영역별 리포트를 받아,
> 구독자가 사전에 등록한 리소스그룹에 매칭하여 매일 10:00 (KST) 봇 DM으로 발송합니다.
>
> · Plugin ID: `ai_service_reporter`
>

---

## 1. 기능 요약

| 요구 | 구현 |
|---|---|
| LLM 분석결과 수신 | `POST /plugins/ai_service_reporter/api/webhook/llm-report` — X-AI-Service-Reporter-Secret 인증 |
| 구독자 매칭 + 발송 | 내부 cron (매일 KST `DeliveryTime`) — 봇 DM 자동 발송 |
| 수신 등록 저장 | `PUT /plugins/ai_service_reporter/api/subscription` |
| 수신 등록 조회 | `GET /plugins/ai_service_reporter/api/subscription` |
| 수신 등록 수정 | `PUT /plugins/ai_service_reporter/api/subscription` (upsert) |
| 수신 등록 삭제 | `DELETE /plugins/ai_service_reporter/api/subscription` (soft delete) |
| 발송 이력 조회 | `GET /plugins/ai_service_reporter/api/delivery-log/me` |
| RHS UI | Apps Bar 아이콘 클릭 → RHS 패널 |

---

## 2. 구성

```
ai-service-reporter-plugin/
├── plugin.json                          # Mattermost manifest
├── Makefile
├── go.mod
├── assets/
│   └── ai_service_reporter.png                        # 봇 프로필 아이콘 (운영 시 추가)
├── server/
│   ├── main.go                          # plugin entry
│   ├── manifest.go                      # in-code manifest
│   ├── configuration.go                 # plugin settings struct
│   ├── plugin.go                        # OnActivate / OnDeactivate
│   ├── api/
│   │   ├── api.go                       # router + 미들웨어
│   │   ├── environment.go
│   │   ├── db_config.go                 # 사내 PG 접속 정보
│   │   ├── db_client.go
│   │   ├── migrations.go                # DDL + 46개 코드 시드
│   │   ├── models.go                    # Subscription/Report/DeliveryLog/...
│   │   ├── subscription_store.go        # CRUD (트랜잭션)
│   │   ├── subscription_routes.go       # GET/PUT/DELETE 핸들러
│   │   ├── resourcegroup_store.go
│   │   ├── resourcegroup_routes.go
│   │   ├── report_store.go              # LLM 리포트 UPSERT
│   │   ├── delivery_log_store.go
│   │   ├── delivery_log_routes.go
│   │   ├── webhook_routes.go            # POST /webhook/llm-report
│   │   ├── scheduler.go                 # 매일 10:00 + 재시도
│   │   └── response.go
│   └── bot/
│       └── bot.go                       # ai_service_reporter + SendDM
└── webapp/
    ├── package.json, tsconfig.json, webpack.config.js, babel.config.js
    └── src/
        ├── index.tsx                    # RHS + Apps Bar 등록
        ├── manifest.ts
        ├── client.ts                    # API 클라이언트
        ├── utils.ts
        ├── types/
        │   ├── ai_service_reporter.ts
        │   └── mattermost-webapp/index.d.ts
        └── components/rhs/
            ├── index.tsx                # 패널 본체
            └── styles.css
```

---

## 3. 빌드

```bash
# 사내 GitLab Runner 또는 로컬
make dist    # → dist/ai_service_reporter.tar.gz
```

서버 시스템 콘솔 → Plugin Management → Upload Plugin → 활성화.
설정에서 `LLMWebhookSecret`, `DeliveryTime`(기본 10:00), `AdminChannelId`(선택) 입력.

---

## 4. DB 스키마

OnActivate 시 자동 마이그레이션. 모든 테이블은 `ai_service_reporter_` 프리픽스.

- `ai_service_reporter_schema_version` — 버전 메타
- `ai_service_reporter_resourcegroups` — 마스터 (46개 코드 시드)
- `ai_service_reporter_subscriptions` — 사용자별 구독
- `ai_service_reporter_subscription_groups` — 구독 ↔ 리소스그룹 N:M
- `ai_service_reporter_reports` — LLM이 보낸 일별 리포트
- `ai_service_reporter_delivery_log` — 발송 결과

기존 Mattermost DB 규칙 준용 (PK varchar(26) nanoid · int8 epoch ms · soft delete `deleteat=0`).

---

## 5. LLM Webhook 페이로드 예시

```http
POST /plugins/ai_service_reporter/api/webhook/llm-report
X-AI-Service-Reporter-Secret: <플러그인 설정값>
Content-Type: application/json

{
  "report_date": "2026-05-13",
  "resource_group": "AC",
  "window_days": 7,
  "anomalies": [
    {
      "case": "I",
      "tr_code": "TR_CUST_INQ_001",
      "tr_name": "고객 조회",
      "diff_ms": 480
    }
  ],
  "content": "안녕하세요! {user.nickname}님! 어제는 최근 1주일 대비 고객 조회 에서 수행시간이 480ms 만큼 더 걸렸어요!"
}
```

- 동일 `(report_date, resource_group)` 중복 수신 시 UPSERT (덮어쓰기)
- `content` 의 `{user.nickname}` 자리표시자는 발송 시 사용자 닉네임으로 자동 치환
- 응답: `202 Accepted` + `{ "report_id": "..." }`

---

## 6. 발송 스케줄

- 매일 `DeliveryTime` (기본 10:00, KST) 도달 시 트리거
- 활성 구독자 별로 그날 들어온 리포트를 매칭
  - `resource_groups`가 비어있으면 전체 수신
  - 채널 지정 시 봇이 멤버여야 발송 가능
- 이미 `sent` 로그가 있는 (user, report) 조합은 skip (멱등성)
- 실패 시 5분 간격 3회 재시도. 최종 실패 → `AdminChannelId` 채널에 알림
- 클러스터 환경: KV Store mutex 로 단일 노드만 발송 보장

---

## 7. 환경별 설정

| 항목 | 개발 | 운영 |
|---|---|---|
| DB Host | `10.16.113.10:15432` | `10.100.112.25:15432` |
| DB Name | `dmattermostdb` | `pmattermostdb` |
| User | `mrmattermost` | `mrmattermost` |

(`server/api/db_config.go` — `reference plugin` 와 동일)

---

## 8. 보안 / 운영 메모

- 모든 사용자 API는 `Mattermost-User-Id` 헤더로 세션 인증 (사용자는 본인 데이터만 접근 가능)
- Webhook은 `X-AI-Service-Reporter-Secret` 헤더로 인증. Secret 미설정 시 401
- 로그는 사용자 ID 앞 8자만 노출 (PII 보호)
- 구독 저장은 트랜잭션 (`ai_service_reporter_subscriptions` + `ai_service_reporter_subscription_groups` 원자성)
- 타임존: KST (`Asia/Seoul`). epoch ms 는 UTC 기준 저장/표시
