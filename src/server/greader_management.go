package server

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
)

func (s *Server) greaderRenameTag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	source, sourceOK := greaderFolderName(r.PostForm.Get("s"))
	destination, destinationOK := greaderFolderName(r.PostForm.Get("dest"))
	if !sourceOK || !destinationOK {
		http.Error(w, "invalid folder label", http.StatusBadRequest)
		return
	}
	if err := s.db.RenameFolderByTitle(source, destination); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "folder label is missing, ambiguous, or already in use", http.StatusBadRequest)
		} else {
			httpError(w, err, http.StatusInternalServerError)
		}
		return
	}
	writeGReaderOK(w)
}

func (s *Server) greaderDisableTag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	name, ok := greaderFolderName(r.PostForm.Get("s"))
	if !ok {
		http.Error(w, "invalid folder label", http.StatusBadRequest)
		return
	}
	if err := s.db.DeleteFolderByTitle(name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "folder label is missing or ambiguous", http.StatusBadRequest)
		} else {
			httpError(w, err, http.StatusInternalServerError)
		}
		return
	}
	writeGReaderOK(w)
}

func (s *Server) greaderSubscriptionImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	outlines, err := readOPMLOutlines(r.Body)
	if err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	if _, err := s.importOutlines(outlines, nil); err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	writeGReaderOK(w)
}

func greaderFolderName(streamID string) (string, bool) {
	name, ok := strings.CutPrefix(streamID, "user/-/label/")
	return name, ok && strings.TrimSpace(name) != ""
}

func writeGReaderOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "OK")
}
