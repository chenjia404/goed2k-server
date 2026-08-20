package ed2ksrv

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monkeyWie/goed2k/protocol"
)

type apiResponse struct {
	OK   bool           `json:"ok"`
	Data any            `json:"data,omitempty"`
	Meta map[string]any `json:"meta,omitempty"`
	Err  string         `json:"error,omitempty"`
}

// ServeAdmin starts the HTTP management interface on the provided listener.
func (s *Server) ServeAdmin(listener net.Listener) error {
	s.mu.Lock()
	s.adminListener = listener
	s.mu.Unlock()
	server := &http.Server{Handler: s.adminMux()}
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) adminMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/clients/", s.handleAdminUI)
	mux.HandleFunc("/files/", s.handleAdminUI)
	mux.HandleFunc("/", s.handleAdminUI)
	mux.HandleFunc("/app.js", s.handleAdminJS)
	mux.HandleFunc("/app.css", s.handleAdminCSS)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/healthz", s.handleHealthz)
	mux.HandleFunc("/api/audit", s.withAdminAuth(s.handleAdminAudit))
	mux.HandleFunc("/api/stats", s.withAdminAuth(s.handleAdminStats))
	mux.HandleFunc("/api/clients", s.withAdminAuth(s.handleAdminClients))
	mux.HandleFunc("/api/clients/", s.withAdminAuth(s.handleAdminClientByID))
	mux.HandleFunc("/api/files/batch-delete", s.withAdminAuth(s.handleAdminBatchDeleteFiles))
	mux.HandleFunc("/api/files", s.withAdminAuth(s.handleAdminFiles))
	mux.HandleFunc("/api/files/", s.withAdminAuth(s.handleAdminFileByHash))
	mux.HandleFunc("/api/persist", s.withAdminAuth(s.handleAdminPersist))
	return mux
}

func (s *Server) withAdminAuth(next http.HandlerFunc) http.HandlerFunc {
	if strings.TrimSpace(s.cfg.AdminToken) == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Admin-Token") != s.cfg.AdminToken {
			writeAPIError(w, http.StatusUnauthorized, fmt.Errorf("unauthorized"))
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	stats := s.StatsSnapshot()
	writeAPI(w, http.StatusOK, map[string]any{
		"status": "ok",
		"uptime": time.Since(stats.StartedAt).String(),
	}, nil)
}

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	stats := s.StatsSnapshot()
	writeAPI(w, http.StatusOK, stats, map[string]any{
		"catalog_path":  s.catalog.Path(),
		"catalog_store": s.catalog.StoreDescription(),
	})
}

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	entries := s.AuditSnapshot()
	page, perPage := parsePagination(r)
	start, end := bounds(len(entries), page, perPage)
	items := []AuditEntry{}
	if start < len(entries) {
		items = entries[start:end]
	}
	writeAPI(w, http.StatusOK, items, pageMeta(page, perPage, len(entries), len(items)))
}

func (s *Server) handleAdminClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	clients := s.ClientsSnapshot()
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	if search != "" {
		filtered := make([]ClientSnapshot, 0, len(clients))
		for _, client := range clients {
			if strings.Contains(strings.ToLower(client.ClientName), search) ||
				strings.Contains(strings.ToLower(client.RemoteAddress), search) ||
				strings.Contains(strings.ToLower(client.ListenEndpoint), search) ||
				strings.Contains(strings.ToLower(client.ClientHash.String()), search) {
				filtered = append(filtered, client)
			}
		}
		clients = filtered
	}
	sortClients(clients, r.URL.Query().Get("sort"))
	items, meta := paginateClients(clients, r)
	writeAPI(w, http.StatusOK, items, meta)
}

func (s *Server) handleAdminClientByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/clients/")
	if rest == "" {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("missing client id"))
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	clientID, err := strconv.ParseInt(parts[0], 10, 32)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("invalid client id: %w", err))
		return
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "revoke-offers":
			if r.Method != http.MethodPost {
				writeMethodNotAllowed(w, http.MethodPost)
				return
			}
			count, err := s.RevokeClientOfferedFiles(int32(clientID))
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, errClientNotFound) {
					status = http.StatusNotFound
				}
				s.appendAudit(AuditEntry{
					Action:     "revoke_offers",
					Resource:   "client",
					ResourceID: parts[0],
					RemoteAddr: r.RemoteAddr,
					Status:     "error",
					Detail:     err.Error(),
				})
				writeAPIError(w, status, err)
				return
			}
			s.appendAudit(AuditEntry{
				Action:     "revoke_offers",
				Resource:   "client",
				ResourceID: parts[0],
				RemoteAddr: r.RemoteAddr,
				Detail:     fmt.Sprintf("revoked=%d", count),
			})
			writeAPI(w, http.StatusOK, map[string]any{
				"client_id":       clientID,
				"revoked_offers": count,
			}, map[string]any{"status": "revoked"})
		default:
			writeAPIError(w, http.StatusNotFound, fmt.Errorf("unknown client action"))
		}
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	client, ok := s.ClientSnapshotByID(int32(clientID))
	if !ok {
		writeAPIError(w, http.StatusNotFound, fmt.Errorf("client not found"))
		return
	}
	writeAPI(w, http.StatusOK, client, nil)
}

func (s *Server) handleAdminFiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		files := s.FilesAdminSnapshot()
		files = filterAdminFiles(files, r)
		sortAdminFiles(files, r.URL.Query().Get("sort"))
		items, meta := paginateAdminFiles(files, r)
		writeAPI(w, http.StatusOK, items, meta)
	case http.MethodPost:
		var record FileRecord
		if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
			s.appendAudit(AuditEntry{Action: "upsert", Resource: "file", RemoteAddr: r.RemoteAddr, Status: "error", Detail: err.Error()})
			writeAPIError(w, http.StatusBadRequest, fmt.Errorf("decode file record: %w", err))
			return
		}
		if err := s.UpsertFile(record); err != nil {
			s.appendAudit(AuditEntry{Action: "upsert", Resource: "file", ResourceID: record.Hash.String(), RemoteAddr: r.RemoteAddr, Status: "error", Detail: err.Error()})
			writeAPIError(w, http.StatusBadRequest, err)
			return
		}
		stored, _ := s.FileAdminSnapshot(record.Hash)
		s.appendAudit(AuditEntry{Action: "upsert", Resource: "file", ResourceID: record.Hash.String(), RemoteAddr: r.RemoteAddr})
		writeAPI(w, http.StatusCreated, stored, map[string]any{"status": "upserted"})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleAdminBatchDeleteFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var request struct {
		Hashes []string `json:"hashes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.appendAudit(AuditEntry{Action: "batch_delete", Resource: "file", RemoteAddr: r.RemoteAddr, Status: "error", Detail: err.Error()})
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("decode batch delete request: %w", err))
		return
	}
	deleted := make([]string, 0, len(request.Hashes))
	missing := make([]string, 0)
	skippedDynamic := make([]string, 0)
	for _, hashText := range request.Hashes {
		hash, err := protocol.HashFromString(hashText)
		if err != nil {
			missing = append(missing, hashText)
			continue
		}
		if s.isDynamicOnlyFile(hash) {
			skippedDynamic = append(skippedDynamic, hash.String())
			continue
		}
		ok, err := s.DeleteFile(hash)
		if err != nil {
			s.appendAudit(AuditEntry{Action: "batch_delete", Resource: "file", ResourceID: hashText, RemoteAddr: r.RemoteAddr, Status: "error", Detail: err.Error()})
			writeAPIError(w, http.StatusInternalServerError, err)
			return
		}
		if ok {
			deleted = append(deleted, hash.String())
			continue
		}
		missing = append(missing, hash.String())
	}
	s.appendAudit(AuditEntry{
		Action:     "batch_delete",
		Resource:   "file",
		RemoteAddr: r.RemoteAddr,
		Detail:     fmt.Sprintf("deleted=%d missing=%d skipped_dynamic=%d", len(deleted), len(missing), len(skippedDynamic)),
	})
	writeAPI(w, http.StatusOK, map[string]any{
		"deleted":         deleted,
		"missing":         missing,
		"skipped_dynamic": skippedDynamic,
	}, map[string]any{"status": "batch_deleted"})
}

func (s *Server) handleAdminFileByHash(w http.ResponseWriter, r *http.Request) {
	hashText := strings.TrimPrefix(r.URL.Path, "/api/files/")
	if hashText == "" {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("missing file hash"))
		return
	}
	hash, err := protocol.HashFromString(hashText)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("invalid file hash: %w", err))
		return
	}
	switch r.Method {
	case http.MethodGet:
		record, ok := s.FileAdminSnapshot(hash)
		if !ok {
			writeAPIError(w, http.StatusNotFound, fmt.Errorf("file not found"))
			return
		}
		writeAPI(w, http.StatusOK, record, nil)
	case http.MethodDelete:
		if s.isDynamicOnlyFile(hash) {
			s.appendAudit(AuditEntry{
				Action:     "delete",
				Resource:   "file",
				ResourceID: hash.String(),
				RemoteAddr: r.RemoteAddr,
				Status:     "error",
				Detail:     "dynamic shared file cannot be deleted via admin API",
			})
			writeAPIError(w, http.StatusConflict, fmt.Errorf("dynamic shared file cannot be deleted via admin API; revoke client offers or disconnect the client"))
			return
		}
		deleted, err := s.DeleteFile(hash)
		if err != nil {
			s.appendAudit(AuditEntry{Action: "delete", Resource: "file", ResourceID: hash.String(), RemoteAddr: r.RemoteAddr, Status: "error", Detail: err.Error()})
			writeAPIError(w, http.StatusInternalServerError, err)
			return
		}
		if !deleted {
			s.appendAudit(AuditEntry{Action: "delete", Resource: "file", ResourceID: hash.String(), RemoteAddr: r.RemoteAddr, Status: "error", Detail: "file not found"})
			writeAPIError(w, http.StatusNotFound, fmt.Errorf("file not found"))
			return
		}
		s.appendAudit(AuditEntry{Action: "delete", Resource: "file", ResourceID: hash.String(), RemoteAddr: r.RemoteAddr})
		writeAPI(w, http.StatusOK, map[string]any{"hash": hash}, map[string]any{"status": "deleted"})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}

func (s *Server) handleAdminPersist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if err := s.PersistCatalog(); err != nil {
		s.appendAudit(AuditEntry{Action: "persist", Resource: "catalog", RemoteAddr: r.RemoteAddr, Status: "error", Detail: err.Error()})
		writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	s.appendAudit(AuditEntry{Action: "persist", Resource: "catalog", RemoteAddr: r.RemoteAddr})
	writeAPI(w, http.StatusOK, map[string]any{"catalog_path": s.catalog.Path()}, map[string]any{"status": "persisted"})
}

func sortClients(clients []ClientSnapshot, field string) {
	switch field {
	case "connected_at":
		sort.Slice(clients, func(i, j int) bool { return clients[i].ConnectedAt.After(clients[j].ConnectedAt) })
	case "last_seen_at":
		sort.Slice(clients, func(i, j int) bool { return clients[i].LastSeenAt.After(clients[j].LastSeenAt) })
	case "name":
		sort.Slice(clients, func(i, j int) bool {
			return strings.ToLower(clients[i].ClientName) < strings.ToLower(clients[j].ClientName)
		})
	default:
		sort.Slice(clients, func(i, j int) bool { return clients[i].ClientID < clients[j].ClientID })
	}
}

func paginateClients(clients []ClientSnapshot, r *http.Request) ([]ClientSnapshot, map[string]any) {
	page, perPage := parsePagination(r)
	start, end := bounds(len(clients), page, perPage)
	items := []ClientSnapshot{}
	if start < len(clients) {
		items = clients[start:end]
	}
	return items, pageMeta(page, perPage, len(clients), len(items))
}

func parsePagination(r *http.Request) (int, int) {
	page := 1
	perPage := 50
	if value, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && value > 0 {
		page = value
	}
	if value, err := strconv.Atoi(r.URL.Query().Get("per_page")); err == nil && value > 0 {
		if value > 500 {
			value = 500
		}
		perPage = value
	}
	return page, perPage
}

func bounds(total, page, perPage int) (int, int) {
	start := (page - 1) * perPage
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return start, end
}

func pageMeta(page, perPage, total, count int) map[string]any {
	return map[string]any{
		"page":     page,
		"per_page": perPage,
		"count":    count,
		"total":    total,
	}
}

func writeAPI(w http.ResponseWriter, status int, data any, meta map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(apiResponse{OK: true, Data: data, Meta: meta}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeAPIError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if encodeErr := json.NewEncoder(w).Encode(apiResponse{OK: false, Err: err.Error()}); encodeErr != nil {
		http.Error(w, encodeErr.Error(), http.StatusInternalServerError)
	}
}

func writeMethodNotAllowed(w http.ResponseWriter, allow ...string) {
	w.Header().Set("Allow", strings.Join(allow, ", "))
	writeAPIError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
}
