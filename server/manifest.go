// This file mirrors the plugin.json manifest. Keep in sync.

package main

import (
	"encoding/json"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

var manifest *model.Manifest

const manifestStr = `
{
  "id": "ai_service_reporter",
  "name": "AI Service Reporter",
  "description": "LLM 분석결과를 구독자에게 매일 10:00 발송하는 사내 리포터 플러그인",
  "homepage_url": "https://gitlab.miraeasset.com/miraeasset/ai-service-reporter-plugin",
  "support_url":  "https://gitlab.miraeasset.com/miraeasset/ai-service-reporter-plugin/issues",
  "icon_path": "assets/ai_service_reporter.png",
  "version": "0.1.0",
  "min_server_version": "6.2.1"
}
`

func init() {
	_ = json.NewDecoder(strings.NewReader(manifestStr)).Decode(&manifest)
}
