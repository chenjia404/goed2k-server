package ed2ksrv

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/monkeyWie/goed2k/protocol"
)

// PublicPeer is a peer entry returned by the public sources API.
type PublicPeer struct {
	Type   string `json:"type"`
	Host   string `json:"host,omitempty"`
	Port   int    `json:"port,omitempty"`
	PeerID string `json:"peer_id,omitempty"`
	Left   int64  `json:"left,omitempty"`
}

// PublicSourcesResponse is the payload for GET /api/v1/files/{hash}/sources.
type PublicSourcesResponse struct {
	Hash       string       `json:"hash"`
	Sources    int          `json:"sources"`
	Complete   int          `json:"complete"`
	Incomplete int          `json:"incomplete"`
	Peers      []PublicPeer `json:"peers"`
}

type announceRequest struct {
	Hash       string `json:"hash"`
	PeerID     string `json:"peer_id"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Uploaded   int64  `json:"uploaded"`
	Downloaded int64  `json:"downloaded"`
	Left       int64  `json:"left"`
	Event      string `json:"event"`
}

type announceResponse struct {
	Interval    int          `json:"interval"`
	MinInterval int          `json:"min_interval"`
	Complete    int          `json:"complete"`
	Incomplete  int          `json:"incomplete"`
	Peers       []PublicPeer `json:"peers"`
}

// ServePublic starts the public HTTP tracker API and Web UI.
func (s *Server) ServePublic(listener net.Listener) error {
	s.ensurePeerStore()
	s.mu.Lock()
	s.publicListener = listener
	s.mu.Unlock()
	server := &http.Server{Handler: s.publicMux()}
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) ensurePeerStore() {
	if s.peerStore != nil {
		return
	}
	s.peerStore = NewPeerStore(time.Duration(s.cfg.PublicPeerTimeout) * time.Second)
	s.peerStore.StartCleanup(time.Minute)
}

func (s *Server) publicMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/healthz", s.handlePublicHealthz)
	mux.HandleFunc("/api/v1/search", s.withPublicAuth(s.handlePublicSearch))
	mux.HandleFunc("/api/v1/announce", s.withPublicAuth(s.handlePublicAnnounce))
	mux.HandleFunc("/api/v1/scrape", s.withPublicAuth(s.handlePublicScrape))
	mux.HandleFunc("/api/v1/files/", s.withPublicAuth(s.handlePublicFileByHash))
	mux.HandleFunc("/file/", s.handlePublicUI)
	mux.HandleFunc("/search", s.handlePublicUI)
	mux.HandleFunc("/app.js", s.handlePublicJS)
	mux.HandleFunc("/app.css", s.handlePublicCSS)
	mux.HandleFunc("/", s.handlePublicUI)
	return mux
}

func (s *Server) withPublicAuth(next http.HandlerFunc) http.HandlerFunc {
	if strings.TrimSpace(s.cfg.PublicHTTPToken) == "" {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Public-Token") != s.cfg.PublicHTTPToken {
			writeAPIError(w, http.StatusUnauthorized, fmt.Errorf("unauthorized"))
			return
		}
		next(w, r)
	}
}

func (s *Server) handlePublicHealthz(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handlePublicSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	query := parsePublicSearchQuery(r)
	files := s.searchFiles(query)
	sortFiles(files, r.URL.Query().Get("sort"))
	page, perPage := parsePublicPagination(r, s.cfg.PublicSearchBatchSize)
	start, end := bounds(len(files), page, perPage)
	items := []FileRecord{}
	if start < len(files) {
		items = files[start:end]
	}
	writeAPI(w, http.StatusOK, items, pageMeta(page, perPage, len(files), len(items)))
}

func (s *Server) handlePublicFileByHash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/files/")
	if rest == "" {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("missing file hash"))
		return
	}
	if strings.HasSuffix(rest, "/sources") {
		hashText := strings.TrimSuffix(rest, "/sources")
		hashText = strings.TrimSuffix(hashText, "/")
		s.handlePublicSources(w, r, hashText)
		return
	}
	hash, err := protocol.HashFromString(rest)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("invalid file hash: %w", err))
		return
	}
	record, ok := s.FileSnapshot(hash)
	if !ok {
		writeAPIError(w, http.StatusNotFound, fmt.Errorf("file not found"))
		return
	}
	writeAPI(w, http.StatusOK, record, nil)
}

func (s *Server) handlePublicSources(w http.ResponseWriter, r *http.Request, hashText string) {
	s.ensurePeerStore()
	hash, err := protocol.HashFromString(hashText)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("invalid file hash: %w", err))
		return
	}
	response := s.publicSources(hash)
	if response.Sources == 0 {
		if _, ok := s.FileSnapshot(hash); !ok && len(s.peerStore.Peers(hash.String())) == 0 {
			writeAPIError(w, http.StatusNotFound, fmt.Errorf("file not found"))
			return
		}
	}
	writeAPI(w, http.StatusOK, response, nil)
}

func (s *Server) handlePublicAnnounce(w http.ResponseWriter, r *http.Request) {
	s.ensurePeerStore()
	var req announceRequest
	switch r.Method {
	case http.MethodPost:
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, fmt.Errorf("decode announce request: %w", err))
			return
		}
	case http.MethodGet:
		req = parseAnnounceQuery(r)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		return
	}
	hash, err := protocol.HashFromString(req.Hash)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("invalid file hash: %w", err))
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("invalid port"))
		return
	}
	if req.Host == "" {
		req.Host = clientHostFromRequest(r)
	}
	peer := HTTPPeer{
		PeerID:     req.PeerID,
		Host:       req.Host,
		Port:       req.Port,
		Uploaded:   req.Uploaded,
		Downloaded: req.Downloaded,
		Left:       req.Left,
		RemoteAddr: r.RemoteAddr,
	}
	event := strings.ToLower(strings.TrimSpace(req.Event))
	switch event {
	case "stopped":
		s.peerStore.Remove(hash.String(), peer)
	case "started", "completed", "":
		if event == "completed" {
			peer.Left = 0
		}
		s.peerStore.Upsert(hash.String(), peer)
	default:
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("unsupported event: %s", req.Event))
		return
	}
	response := s.publicAnnounceResponse(hash)
	writeAPI(w, http.StatusOK, response, nil)
}

func (s *Server) handlePublicScrape(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	hashes := r.URL.Query()["hash"]
	if len(hashes) == 0 {
		writeAPIError(w, http.StatusBadRequest, fmt.Errorf("missing hash parameter"))
		return
	}
	files := make(map[string]map[string]int)
	for _, hashText := range hashes {
		hash, err := protocol.HashFromString(hashText)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, fmt.Errorf("invalid file hash: %w", err))
			return
		}
		stats := s.publicScrapeStats(hash)
		files[hash.String()] = stats
	}
	writeAPI(w, http.StatusOK, map[string]any{"files": files}, nil)
}

func (s *Server) searchFiles(query SearchQuery) []FileRecord {
	results := make([]FileRecord, 0)
	for _, entry := range s.catalog.Search(query) {
		if record, ok := s.catalog.Get(entry.Hash); ok {
			results = append(results, record)
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, shared := range s.dynamicFiles {
		record := shared.materialize()
		if matchesRecord(record, query) {
			results = append(results, record)
		}
	}
	return results
}

func (s *Server) publicSources(hash protocol.Hash) PublicSourcesResponse {
	s.ensurePeerStore()
	hashText := hash.String()
	peers := make([]PublicPeer, 0)
	complete := 0
	incomplete := 0

	for _, endpoint := range s.sourcesAll(hash) {
		host := endpoint.String()
		if addr, err := endpoint.ToTCPAddr(); err == nil {
			host = addr.IP.String()
		}
		peers = append(peers, PublicPeer{
			Type: "ed2k",
			Host: host,
			Port: endpoint.Port(),
		})
	}
	if record, ok := s.FileSnapshot(hash); ok {
		complete = record.CompleteSources
		incomplete = record.Sources - record.CompleteSources
		if incomplete < 0 {
			incomplete = 0
		}
	}
	for _, peer := range s.peerStore.Peers(hashText) {
		peers = append(peers, PublicPeer{
			Type:   "http",
			Host:   peer.Host,
			Port:   peer.Port,
			PeerID: peer.PeerID,
			Left:   peer.Left,
		})
		if peer.Left == 0 {
			complete++
		} else {
			incomplete++
		}
	}
	return PublicSourcesResponse{
		Hash:       hashText,
		Sources:    len(peers),
		Complete:   complete,
		Incomplete: incomplete,
		Peers:      peers,
	}
}

func (s *Server) publicAnnounceResponse(hash protocol.Hash) announceResponse {
	sources := s.publicSources(hash)
	peers := sources.Peers
	maxPeers := s.cfg.PublicMaxPeersReturned
	if maxPeers > 0 && len(peers) > maxPeers {
		peers = peers[:maxPeers]
	}
	return announceResponse{
		Interval:    s.cfg.PublicAnnounceInterval,
		MinInterval: s.cfg.PublicMinAnnounceInterval,
		Complete:    sources.Complete,
		Incomplete:  sources.Incomplete,
		Peers:       peers,
	}
}

func (s *Server) publicScrapeStats(hash protocol.Hash) map[string]int {
	sources := s.publicSources(hash)
	return map[string]int{
		"complete":   sources.Complete,
		"incomplete": sources.Incomplete,
		"downloaded": 0,
	}
}

func parsePublicSearchQuery(r *http.Request) SearchQuery {
	query := SearchQuery{}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		query.Keywords = splitSearchTerms(q)
	}
	if exclude := strings.TrimSpace(r.URL.Query().Get("exclude")); exclude != "" {
		query.ExcludedKeywords = splitSearchTerms(exclude)
	}
	query.FileType = strings.TrimSpace(r.URL.Query().Get("type"))
	query.Extension = strings.TrimSpace(r.URL.Query().Get("ext"))
	if value, err := strconv.ParseInt(r.URL.Query().Get("min_size"), 10, 64); err == nil && value > 0 {
		query.MinSize = value
	}
	if value, err := strconv.ParseInt(r.URL.Query().Get("max_size"), 10, 64); err == nil && value > 0 {
		query.MaxSize = value
	}
	if value, err := strconv.Atoi(r.URL.Query().Get("min_sources")); err == nil && value > 0 {
		query.MinSources = value
	}
	return query
}

func parsePublicPagination(r *http.Request, defaultPerPage int) (int, int) {
	page := 1
	perPage := defaultPerPage
	if perPage <= 0 {
		perPage = defaultPublicSearchBatchSize
	}
	if value, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && value > 0 {
		page = value
	}
	if value, err := strconv.Atoi(r.URL.Query().Get("per_page")); err == nil && value > 0 {
		perPage = value
	}
	if perPage > 500 {
		perPage = 500
	}
	return page, perPage
}

func splitSearchTerms(value string) []string {
	parts := strings.Fields(value)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseAnnounceQuery(r *http.Request) announceRequest {
	port, _ := strconv.Atoi(r.URL.Query().Get("port"))
	uploaded, _ := strconv.ParseInt(r.URL.Query().Get("uploaded"), 10, 64)
	downloaded, _ := strconv.ParseInt(r.URL.Query().Get("downloaded"), 10, 64)
	left, _ := strconv.ParseInt(r.URL.Query().Get("left"), 10, 64)
	return announceRequest{
		Hash:       r.URL.Query().Get("hash"),
		PeerID:     r.URL.Query().Get("peer_id"),
		Host:       r.URL.Query().Get("host"),
		Port:       port,
		Uploaded:   uploaded,
		Downloaded: downloaded,
		Left:       left,
		Event:      r.URL.Query().Get("event"),
	}
}

func clientHostFromRequest(r *http.Request) string {
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if host != "" {
		if idx := strings.Index(host, ","); idx >= 0 {
			host = strings.TrimSpace(host[:idx])
		}
		if parsed := net.ParseIP(host); parsed != nil {
			return parsed.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func ed2kLink(record FileRecord) string {
	return fmt.Sprintf("ed2k://|file|%s|%d|%s|/", record.Name, record.Size, record.Hash.String())
}
