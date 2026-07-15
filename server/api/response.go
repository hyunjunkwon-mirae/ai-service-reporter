package api

import (
	"encoding/json"
	"net/http"
)

// sendJSON은 JSON 응답을 전송합니다.
func sendJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}

// sendError는 표준 에러 응답을 전송합니다.
func sendError(w http.ResponseWriter, statusCode int, code, message string) {
	sendJSON(w, statusCode, map[string]interface{}{
		"success": false,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
