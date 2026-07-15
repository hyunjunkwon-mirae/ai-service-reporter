-- =============================================================================
-- AI Service Reporter Plugin — 초기 DB 스키마 (참고용)
--
-- 운영 정책:
--   · DBA 가 수동으로 실행
--   · 플러그인은 DDL 을 발급하지 않음
--   · 컬럼명/타입 변경 시 server/api/*_store.go 의 SQL 도 함께 수정 필요
--
-- 스키마: mrmattermost
-- 환경:   dev (10.16.113.10:15432/dmattermostdb), prod (10.100.112.25:15432/pmattermostdb)
-- 권한:   mrmattermost
-- =============================================================================

-- -----------------------------------------------------------------------------
-- 1) 업무영역(리소스그룹) 마스터
-- -----------------------------------------------------------------------------
CREATE TABLE mrmattermost.ai_service_reporter_resourcegroups (
    code        varchar(8)   NOT NULL,
    "name"      varchar(100) NULL,
    definition  text         NULL,
    sortorder   int4         NULL,
    active      bool         NULL,
    createat    int8         NULL,
    updateat    int8         NULL,
    CONSTRAINT pk_ai_service_reporter_resourcegroups PRIMARY KEY (code)
);

-- -----------------------------------------------------------------------------
-- 2) 구독 (사용자 단위)
-- -----------------------------------------------------------------------------
CREATE TABLE mrmattermost.ai_service_reporter_subscriptions (
    id        varchar(26) NOT NULL,
    userid    varchar(26) NOT NULL,
    active    bool        NOT NULL,
    channelid varchar(26) NULL,
    createat  int8        NULL,
    updateat  int8        NULL,
    deleteat  int8        NULL,
    CONSTRAINT pk_ai_service_reporter_subscriptions PRIMARY KEY (id)
);

-- -----------------------------------------------------------------------------
-- 3) 구독-리소스그룹 매핑 (N:M)
-- -----------------------------------------------------------------------------
CREATE TABLE mrmattermost.ai_service_reporter_subscription_groups (
    subscriptionid    varchar(26) NOT NULL,
    resourcegroupcode varchar(8)  NOT NULL,
    createat          int8        NULL,
    CONSTRAINT pk_ai_service_reporter_subscription_groups PRIMARY KEY (subscriptionid, resourcegroupcode)
);

-- -----------------------------------------------------------------------------
-- 4) 리포트 (일별 / 업무영역별 발송 본문)
--    ⚠️ (reportdate, resourcegroupcode) 에 UNIQUE 제약 없음 →
--       server/api/report_store.go 의 Upsert 는 SELECT-then-UPDATE 패턴 사용
-- -----------------------------------------------------------------------------
CREATE TABLE mrmattermost.ai_service_reporter_reports (
    id                varchar(26) NOT NULL,
    reportdate        date        NOT NULL,
    resourcegroupcode varchar(8)  NOT NULL,
    windowdays        int4        NOT NULL,
    "content"         text        NULL,
    rawdata           jsonb       NULL,
    createat          int8        NULL,
    updateat          int8        NULL,
    CONSTRAINT pk_ai_service_reporter_reports PRIMARY KEY (id)
);

-- -----------------------------------------------------------------------------
-- 5) 발송 이력
-- -----------------------------------------------------------------------------
CREATE TABLE mrmattermost.ai_service_reporter_delivery_log (
    id             varchar(26) NOT NULL,
    userid         varchar(26) NULL,
    subscriptionid varchar(26) NOT NULL,
    reportid       varchar(26) NOT NULL,
    channelid      varchar(26) NULL,
    status         varchar(16) NULL,
    retrycount     int4        NULL,
    error          text        NULL,
    sentat         int8        NULL,
    createat       int8        NULL,
    updateat       int8        NULL,
    CONSTRAINT pk_ai_service_reporter_delivery_log PRIMARY KEY (id)
);

-- =============================================================================
-- DROP (참고)
-- =============================================================================
-- DROP TABLE IF EXISTS mrmattermost.ai_service_reporter_delivery_log;
-- DROP TABLE IF EXISTS mrmattermost.ai_service_reporter_subscription_groups;
-- DROP TABLE IF EXISTS mrmattermost.ai_service_reporter_subscriptions;
-- DROP TABLE IF EXISTS mrmattermost.ai_service_reporter_reports;
-- DROP TABLE IF EXISTS mrmattermost.ai_service_reporter_resourcegroups;
