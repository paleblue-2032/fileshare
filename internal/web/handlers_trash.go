package web

import (
	"net/http"
)

func (s *Server) handleTrashList(w http.ResponseWriter, r *http.Request) {
	items := s.store.TrashList()
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"totalBytes": s.store.TrashTotalBytes(),
	})
}

func (s *Server) handleTrashRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	entry, err := s.store.Restore(req.ID)
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) handleTrashPurge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.store.Purge(req.ID); err != nil {
		s.handleStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTrashEmpty(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.EmptyTrash()
	if err != nil {
		s.handleStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
}
