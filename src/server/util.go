package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"forge.harakara.site/littleisland/hayari/src/storage"
)

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error, code int) {
	http.Error(w, err.Error(), code)
}

func encodeItemCursor(date string, id int64) string {
	payload := date + "|" + strconv.FormatInt(id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeItemCursor(value string) (*storage.ItemCursor, error) {
	if value == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	date, rawID, ok := strings.Cut(string(payload), "|")
	if !ok || date == "" {
		return nil, fmt.Errorf("invalid continuation")
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		return nil, fmt.Errorf("invalid continuation")
	}
	return &storage.ItemCursor{Date: date, ID: id}, nil
}
