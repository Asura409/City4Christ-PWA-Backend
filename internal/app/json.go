package app

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var zero T
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, 415, "unsupported_media_type", "Use application/json.", nil)
		return zero, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input *T
	if err := decoder.Decode(&input); err != nil {
		writeDecodeError(w, err)
		return zero, false
	}
	if input == nil {
		writeDecodeError(w, nil)
		return zero, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeDecodeError(w, err)
		return zero, false
	}
	return *input, true
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, 413, "body_too_large", "Request body is too large.", nil)
		return
	}
	writeError(w, 400, "invalid_json", "Supply a single valid JSON object.", nil)
}

func writeError(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	if fields == nil {
		fields = map[string]string{}
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "fields": fields, "requestId": w.Header().Get("X-Request-Id")}})
}
