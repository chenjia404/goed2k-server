package ed2ksrv

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monkeyWie/goed2k/protocol"
	serverproto "github.com/monkeyWie/goed2k/protocol/server"
)

// ServerStats is the admin-facing runtime metrics snapshot.
type ServerStats struct {
	StartedAt           time.Time `json:"started_at"`
	CurrentClients      int       `json:"current_clients"`
	CurrentFiles        int       `json:"current_files"`
	TotalConnections    int64     `json:"total_connections"`
	TotalDisconnects    int64     `json:"total_disconnects"`
	InboundPackets      int64     `json:"inbound_packets"`
	OutboundPackets     int64     `json:"outbound_packets"`
	InboundBytes        int64     `json:"inbound_bytes"`
	OutboundBytes       int64     `json:"outbound_bytes"`
	SearchRequests      int64     `json:"search_requests"`
	SearchResultPackets int64     `json:"search_result_packets"`
	SearchResultEntries int64     `json:"search_result_entries"`
	SourceRequests      int64     `json:"source_requests"`
	CallbackRequests    int64     `json:"callback_requests"`
	FilesRegistered     int64     `json:"files_registered"`
	FilesRemoved        int64     `json:"files_removed"`
	PersistWrites       int64     `json:"persist_writes"`
}

// ClientSnapshot is the admin-facing view of a connected client.
type ClientSnapshot struct {
	ClientID         int32         `json:"client_id"`
	ClientName       string        `json:"client_name"`
	ClientHash       protocol.Hash `json:"client_hash"`
	RemoteAddress    string        `json:"remote_address"`
	ListenEndpoint   string        `json:"listen_endpoint"`
	ConnectedAt      time.Time     `json:"connected_at"`
	LastSeenAt       time.Time     `json:"last_seen_at"`
	SearchRequests   int64         `json:"search_requests"`
	SourceRequests   int64         `json:"source_requests"`
	CallbackRequests int64         `json:"callback_requests"`
	InboundPackets   int64         `json:"inbound_packets"`
	OutboundPackets  int64         `json:"outbound_packets"`
	InboundBytes     int64         `json:"inbound_bytes"`
	OutboundBytes    int64         `json:"outbound_bytes"`
}

// Server exposes the subset of the ED2K/eMule server protocol used by goed2k.
type Server struct {
	cfg      Config
	catalog  *Catalog
	logger   *slog.Logger
	combiner protocol.PacketCombiner

	mu            sync.RWMutex
	listener      net.Listener
	udpConn       *net.UDPConn
	adminListener net.Listener
	clients       map[int32]*clientSession
	dynamicFiles  map[string]*dynamicSharedFile
	auditLog      []AuditEntry
	closed        chan struct{}
	nextID        int32
	startedAt     time.Time
	stats         serverCounters
}

type dynamicSharedFile struct {
	record    FileRecord
	byClient  map[int32]SourceEntry
	completes map[int32]bool
}

type serverCounters struct {
	TotalConnections    int64
	TotalDisconnects    int64
	InboundPackets      int64
	OutboundPackets     int64
	InboundBytes        int64
	OutboundBytes       int64
	SearchRequests      int64
	SearchResultPackets int64
	SearchResultEntries int64
	SourceRequests      int64
	CallbackRequests    int64
	FilesRegistered     int64
	FilesRemoved        int64
	PersistWrites       int64
}

type clientSession struct {
	server *Server
	conn   net.Conn
	remote *net.TCPAddr

	writeMu sync.Mutex
	mu      sync.Mutex

	clientHash       protocol.Hash
	loginPoint       protocol.Endpoint
	assignedID       int32
	clientName       string
	offeredFiles     map[string]FileRecord
	searchResult     []serverproto.SharedFileEntry
	searchOffset     int
	connectedAt      time.Time
	lastSeenAt       time.Time
	searchRequests   int64
	sourceRequests   int64
	callbackRequests int64
	inboundPackets   int64
	outboundPackets  int64
	inboundBytes     int64
	outboundBytes    int64
}

// NewServer constructs a ready-to-run server instance.
func NewServer(cfg Config, catalog *Catalog, logger *slog.Logger) (*Server, error) {
	normalized, err := cfg.Normalize()
	if err != nil {
		return nil, err
	}
	if catalog == nil {
		catalog, err = LoadCatalogFromConfig(normalized)
		if err != nil {
			return nil, err
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		cfg:          normalized,
		catalog:      catalog,
		logger:       logger,
		combiner:     serverproto.NewPacketCombiner(),
		clients:      make(map[int32]*clientSession),
		dynamicFiles: make(map[string]*dynamicSharedFile),
		closed:       make(chan struct{}),
		nextID:       16777217,
		startedAt:    time.Now(),
	}, nil
}

// Config returns the normalized runtime configuration.
func (s *Server) Config() Config {
	return s.cfg
}

// ListenAndServe opens the configured TCP and HTTP admin listeners.
func (s *Server) ListenAndServe() error {
	if s.cfg.AdminListenAddress != "" {
		adminListener, err := net.Listen("tcp", s.cfg.AdminListenAddress)
		if err != nil {
			return err
		}
		go func() {
			if err := s.ServeAdmin(adminListener); err != nil && !errors.Is(err, net.ErrClosed) {
				s.logger.Error("admin server stopped", "err", err)
			}
		}()
	}
	listener, err := net.Listen("tcp", s.cfg.ListenAddress)
	if err != nil {
		return err
	}
	return s.Serve(listener)
}

// Serve accepts ED2K connections on an existing listener.
func (s *Server) Serve(listener net.Listener) error {
	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()
	s.maybeStartServerUDP()
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return nil
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return err
		}
		go s.handleConn(conn)
	}
}

// Shutdown stops the listeners and closes all active client connections.
func (s *Server) Shutdown(ctx context.Context) error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}

	s.mu.Lock()
	listener := s.listener
	udpConn := s.udpConn
	s.udpConn = nil
	adminListener := s.adminListener
	clients := make([]*clientSession, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.Unlock()

	if udpConn != nil {
		_ = udpConn.Close()
	}
	if listener != nil {
		_ = listener.Close()
	}
	if adminListener != nil {
		_ = adminListener.Close()
	}
	if s.catalog != nil {
		_ = s.catalog.Close()
	}
	for _, client := range clients {
		_ = client.conn.Close()
	}
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// StatsSnapshot returns a point-in-time runtime metrics view.
func (s *Server) StatsSnapshot() ServerStats {
	s.mu.RLock()
	stats := ServerStats{
		StartedAt:           s.startedAt,
		CurrentClients:      len(s.clients),
		CurrentFiles:        s.catalog.Count() + len(s.dynamicFiles),
		TotalConnections:    s.stats.TotalConnections,
		TotalDisconnects:    s.stats.TotalDisconnects,
		InboundPackets:      s.stats.InboundPackets,
		OutboundPackets:     s.stats.OutboundPackets,
		InboundBytes:        s.stats.InboundBytes,
		OutboundBytes:       s.stats.OutboundBytes,
		SearchRequests:      s.stats.SearchRequests,
		SearchResultPackets: s.stats.SearchResultPackets,
		SearchResultEntries: s.stats.SearchResultEntries,
		SourceRequests:      s.stats.SourceRequests,
		CallbackRequests:    s.stats.CallbackRequests,
		FilesRegistered:     s.stats.FilesRegistered,
		FilesRemoved:        s.stats.FilesRemoved,
		PersistWrites:       s.stats.PersistWrites,
	}
	s.mu.RUnlock()
	return stats
}

// ClientsSnapshot returns the current dynamic user table.
func (s *Server) ClientsSnapshot() []ClientSnapshot {
	s.mu.RLock()
	clients := make([]*clientSession, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.RUnlock()
	snapshots := make([]ClientSnapshot, 0, len(clients))
	for _, client := range clients {
		snapshots = append(snapshots, client.snapshot())
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].ClientID < snapshots[j].ClientID
	})
	return snapshots
}

// FilesSnapshot returns a copy of all shared file entries.
func (s *Server) FilesSnapshot() []FileRecord {
	files := s.catalog.Snapshot()
	s.mu.RLock()
	for _, shared := range s.dynamicFiles {
		files = append(files, cloneFiles([]FileRecord{shared.materialize()})...)
	}
	s.mu.RUnlock()
	return files
}

// FileSnapshot returns one shared file record by hash.
func (s *Server) FileSnapshot(hash protocol.Hash) (FileRecord, bool) {
	if record, ok := s.catalog.Get(hash); ok {
		return record, true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	shared, ok := s.dynamicFiles[hash.String()]
	if !ok {
		return FileRecord{}, false
	}
	return shared.materialize(), true
}

// ClientSnapshotByID returns one connected client by assigned ID.
func (s *Server) ClientSnapshotByID(clientID int32) (ClientSnapshot, bool) {
	s.mu.RLock()
	client := s.clients[clientID]
	s.mu.RUnlock()
	if client == nil {
		return ClientSnapshot{}, false
	}
	return client.snapshot(), true
}

// UpsertFile registers or replaces a shared file and persists it to disk.
func (s *Server) UpsertFile(record FileRecord) error {
	if err := s.catalog.Upsert(record); err != nil {
		return err
	}
	s.mu.Lock()
	s.stats.FilesRegistered++
	s.mu.Unlock()
	return s.persistCatalog()
}

// DeleteFile removes a shared file and persists the new catalog.
func (s *Server) DeleteFile(hash protocol.Hash) (bool, error) {
	deleted := s.catalog.Delete(hash)
	if !deleted {
		return false, nil
	}
	s.mu.Lock()
	s.stats.FilesRemoved++
	s.mu.Unlock()
	if err := s.persistCatalog(); err != nil {
		return false, err
	}
	return true, nil
}

// PersistCatalog writes the current runtime catalog to disk.
func (s *Server) PersistCatalog() error {
	return s.persistCatalog()
}

func (s *Server) handleConn(conn net.Conn) {
	first := make([]byte, 1)
	if _, err := io.ReadFull(conn, first); err != nil {
		_ = conn.Close()
		return
	}
	if s.cfg.ProtocolObfuscation && !isPlainEd2kFirstByte(first[0]) {
		c2, err := serverObfuscatedHandshake(conn, first[0])
		if err != nil {
			s.logger.Warn("obfuscation handshake failed", "remote", conn.RemoteAddr().String(), "err", err)
			_ = conn.Close()
			return
		}
		conn = c2
	} else {
		if !isPlainEd2kFirstByte(first[0]) {
			s.logger.Warn("rejected non-ed2k first byte (protocol_obfuscation is false)", "remote", conn.RemoteAddr().String())
			_ = conn.Close()
			return
		}
		conn = &prependConn{Conn: conn, prefix: []byte{first[0]}}
	}

	tcpAddr, _ := conn.RemoteAddr().(*net.TCPAddr)
	client := &clientSession{
		server:       s,
		conn:         conn,
		remote:       tcpAddr,
		offeredFiles: make(map[string]FileRecord),
		connectedAt:  time.Now(),
		lastSeenAt:   time.Now(),
	}
	s.bumpCounter(func(stats *serverCounters) {
		stats.TotalConnections++
	})
	defer func() {
		s.unregisterClient(client)
		_ = conn.Close()
	}()

	s.logger.Info("client connected", "remote", conn.RemoteAddr().String())
	for {
		header, body, frameBytes, err := readFrame(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				s.logger.Warn("connection closed", "remote", conn.RemoteAddr().String(), "err", err)
			}
			return
		}
		client.noteInbound(frameBytes)
		s.bumpCounter(func(stats *serverCounters) {
			stats.InboundPackets++
			stats.InboundBytes += int64(frameBytes)
		})
		if header.Protocol != protocol.EdonkeyHeader {
			s.logger.Warn("unsupported protocol", "protocol", header.Protocol, "remote", conn.RemoteAddr().String())
			return
		}
		if err := s.dispatch(client, header.Packet, body); err != nil {
			s.logger.Warn("request handling failed", "packet", fmt.Sprintf("0x%02x", header.Packet), "remote", conn.RemoteAddr().String(), "err", err)
			return
		}
	}
}

func (s *Server) dispatch(client *clientSession, packet byte, body []byte) error {
	switch packet {
	case opLoginRequest:
		var req serverproto.LoginRequest
		if err := req.Get(bytes.NewReader(body)); err != nil {
			return err
		}
		return s.handleLogin(client, req)
	case opGetServerList:
		return s.handleGetServerList(client)
	case opOfferFiles:
		var req OfferFiles
		if err := req.Get(bytes.NewReader(body)); err != nil {
			return err
		}
		return s.handleOfferFiles(client, req)
	case opSearchRequest:
		query, err := ParseSearchRequest(body)
		if err != nil {
			return err
		}
		return s.handleSearch(client, query)
	case opSearchMore:
		return s.handleSearchMore(client)
	case opGetSources:
		var req serverproto.GetFileSources
		if err := req.Get(bytes.NewReader(body)); err != nil {
			return err
		}
		return s.handleGetSources(client, req, false)
	case opGetSourcesObfu:
		var req serverproto.GetFileSources
		if err := req.Get(bytes.NewReader(body)); err != nil {
			return err
		}
		return s.handleGetSources(client, req, true)
	case opCallbackReq:
		var req serverproto.CallbackRequest
		if err := req.Get(bytes.NewReader(body)); err != nil {
			return err
		}
		return s.handleCallback(client, req)
	case opDisconnect:
		return io.EOF
	default:
		s.logger.Warn("unsupported packet", "packet", fmt.Sprintf("0x%02x", packet), "remote", client.conn.RemoteAddr().String())
		return nil
	}
}

func (s *Server) handleLogin(client *clientSession, req serverproto.LoginRequest) error {
	client.mu.Lock()
	client.clientHash = req.Hash
	client.loginPoint = req.Point
	client.clientName = extractClientName(req.Properties)
	client.assignedID = s.allocateClientID(client.remote)
	client.searchResult = nil
	client.searchOffset = 0
	assignedID := client.assignedID
	client.mu.Unlock()

	s.registerClient(client)

	if err := client.send("server.Status", &serverproto.Status{UsersCount: int32(s.clientCount()), FilesCount: int32(s.currentFilesCount())}); err != nil {
		return err
	}
	if s.cfg.Message != "" {
		if err := client.send("server.Message", &serverproto.Message{Value: protocol.ByteContainer16FromString(s.cfg.Message)}); err != nil {
			return err
		}
	}
	ic := idChangeExtended{
		ClientID:             assignedID,
		TCPFlags:             s.cfg.TCPFlags,
		AuxPort:              s.cfg.AuxPort,
		ReportedIP:           reportedIPForIdChange(assignedID),
		ObfuscationTCPPort:   obfuscationTCPPortAdvertised(s.cfg, s.serverTCPPort()),
	}
	return client.send("server.IdChange", &ic)
}

func (s *Server) handleGetServerList(client *clientSession) error {
	if err := client.send("server.Status", &serverproto.Status{UsersCount: int32(s.clientCount()), FilesCount: int32(s.currentFilesCount())}); err != nil {
		return err
	}
	if s.cfg.ServerDescription == "" {
		return nil
	}
	return client.send("server.Message", &serverproto.Message{Value: protocol.ByteContainer16FromString(s.cfg.ServerDescription)})
}

func (s *Server) handleOfferFiles(client *clientSession, req OfferFiles) error {
	client.mu.Lock()
	clientID := client.assignedID
	listenPort := client.loginPoint.Port()
	client.mu.Unlock()
	if clientID == 0 {
		return fmt.Errorf("client must login before offering files")
	}
	records := make([]FileRecord, 0, len(req.Entries))
	for _, entry := range req.Entries {
		record, err := fileRecordFromSharedEntry(entry)
		if err != nil {
			return err
		}
		source, ok := sourceFromSharedEntry(clientID, listenPort, entry)
		if ok {
			record.Endpoints = []SourceEntry{source}
		}
		records = append(records, record)
	}
	s.replaceClientOfferedFiles(clientID, records)
	s.bumpCounter(func(stats *serverCounters) {
		stats.FilesRegistered += int64(len(records))
	})
	return client.send("server.Status", &serverproto.Status{UsersCount: int32(s.clientCount()), FilesCount: int32(s.currentFilesCount())})
}

func (s *Server) handleSearch(client *clientSession, query SearchQuery) error {
	results := s.searchAll(query)
	client.noteSearchRequest()
	s.bumpCounter(func(stats *serverCounters) {
		stats.SearchRequests++
		stats.SearchResultEntries += int64(len(results))
	})
	client.mu.Lock()
	client.searchResult = results
	client.searchOffset = 0
	client.mu.Unlock()
	return s.handleSearchMore(client)
}

func (s *Server) handleSearchMore(client *clientSession) error {
	client.mu.Lock()
	if len(client.searchResult) == 0 || client.searchOffset >= len(client.searchResult) {
		client.mu.Unlock()
		s.bumpCounter(func(stats *serverCounters) {
			stats.SearchResultPackets++
		})
		return client.send("server.SearchResult", &serverproto.SearchResult{Results: nil, MoreResults: false})
	}
	end := client.searchOffset + s.cfg.SearchBatchSize
	if end > len(client.searchResult) {
		end = len(client.searchResult)
	}
	packet := &serverproto.SearchResult{
		Results:     append([]serverproto.SharedFileEntry(nil), client.searchResult[client.searchOffset:end]...),
		MoreResults: end < len(client.searchResult),
	}
	client.searchOffset = end
	client.mu.Unlock()
	s.bumpCounter(func(stats *serverCounters) {
		stats.SearchResultPackets++
	})
	return client.send("server.SearchResult", packet)
}

func (s *Server) handleGetSources(client *clientSession, req serverproto.GetFileSources, obfuscatedReply bool) error {
	sources := s.sourcesAll(req.Hash)
	if len(sources) > 255 {
		sources = sources[:255]
	}
	client.noteSourceRequest()
	s.bumpCounter(func(stats *serverCounters) {
		stats.SourceRequests++
	})
	if obfuscatedReply {
		return client.sendFoundSourcesObfuscated(req.Hash, sources)
	}
	return client.send("server.FoundFileSources", &serverproto.FoundFileSources{Hash: req.Hash, Sources: sources})
}

func (s *Server) handleCallback(client *clientSession, req serverproto.CallbackRequest) error {
	client.noteCallbackRequest()
	s.bumpCounter(func(stats *serverCounters) {
		stats.CallbackRequests++
	})
	target := s.findClient(req.ClientID)
	if target == nil {
		return client.send("server.CallbackRequestFailed", &serverproto.CallbackRequestFailed{})
	}
	client.mu.Lock()
	origin := protocol.NewEndpoint(client.assignedID, client.loginPoint.Port())
	client.mu.Unlock()
	if err := target.send("server.CallbackRequestIncoming", &serverproto.CallbackRequestIncoming{Point: origin}); err != nil {
		return client.send("server.CallbackRequestFailed", &serverproto.CallbackRequestFailed{})
	}
	return nil
}

func (s *Server) registerClient(client *clientSession) {
	client.mu.Lock()
	assignedID := client.assignedID
	client.mu.Unlock()
	if assignedID == 0 {
		return
	}
	s.mu.Lock()
	s.clients[assignedID] = client
	s.mu.Unlock()
}

func (s *Server) unregisterClient(client *clientSession) {
	client.mu.Lock()
	assignedID := client.assignedID
	client.mu.Unlock()
	s.bumpCounter(func(stats *serverCounters) {
		stats.TotalDisconnects++
	})
	if assignedID != 0 {
		s.removeClientOfferedFiles(assignedID)
	}
	if assignedID == 0 {
		return
	}
	s.mu.Lock()
	if existing := s.clients[assignedID]; existing == client {
		delete(s.clients, assignedID)
	}
	s.mu.Unlock()
}

func (s *Server) findClient(clientID int32) *clientSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clients[clientID]
}

func (s *Server) clientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

func (s *Server) allocateClientID(addr *net.TCPAddr) int32 {
	if clientID := clientIDFromRemote(addr); clientID != 0 {
		return clientID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	clientID := s.nextID
	s.nextID++
	return clientID
}

// serverTCPPort 返回当前 ED2K 监听 TCP 端口，用于 IdChange 中通告混淆端口。
func (s *Server) serverTCPPort() uint16 {
	s.mu.RLock()
	ln := s.listener
	s.mu.RUnlock()
	if ln != nil {
		if ta, ok := ln.Addr().(*net.TCPAddr); ok && ta.Port > 0 {
			return uint16(ta.Port)
		}
	}
	if a, err := net.ResolveTCPAddr("tcp", s.cfg.ListenAddress); err == nil && a.Port > 0 {
		return uint16(a.Port)
	}
	return 4661
}

func (s *Server) currentFilesCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.catalog.Count() + len(s.dynamicFiles)
}

func (s *Server) searchAll(query SearchQuery) []serverproto.SharedFileEntry {
	results := s.catalog.Search(query)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, shared := range s.dynamicFiles {
		record := shared.materialize()
		if !matchesRecord(record, query) {
			continue
		}
		results = append(results, makeSharedFileEntry(record))
	}
	return results
}

func (s *Server) sourcesAll(hash protocol.Hash) []protocol.Endpoint {
	sources := s.catalog.Sources(hash)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if shared, ok := s.dynamicFiles[hash.String()]; ok {
		record := shared.materialize()
		for _, source := range record.Endpoints {
			endpoint, err := protocol.EndpointFromString(source.Host, source.Port)
			if err == nil {
				sources = append(sources, endpoint)
			}
		}
	}
	return sources
}

func (s *Server) replaceClientOfferedFiles(clientID int32, records []FileRecord) {
	client := s.findClient(clientID)
	if client == nil {
		return
	}
	client.mu.Lock()
	previous := make([]FileRecord, 0, len(client.offeredFiles))
	for _, record := range client.offeredFiles {
		previous = append(previous, record)
	}
	client.offeredFiles = make(map[string]FileRecord, len(records))
	for _, record := range records {
		client.offeredFiles[record.Hash.String()] = record
	}
	client.mu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range previous {
		s.removeDynamicLocked(clientID, record.Hash)
	}
	for _, record := range records {
		s.addDynamicLocked(clientID, record)
	}
}

func (s *Server) removeClientOfferedFiles(clientID int32) {
	client := s.findClient(clientID)
	if client == nil {
		return
	}
	client.mu.Lock()
	records := make([]FileRecord, 0, len(client.offeredFiles))
	for _, record := range client.offeredFiles {
		records = append(records, record)
	}
	client.offeredFiles = map[string]FileRecord{}
	client.mu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range records {
		s.removeDynamicLocked(clientID, record.Hash)
	}
}

func (s *Server) addDynamicLocked(clientID int32, record FileRecord) {
	key := record.Hash.String()
	shared := s.dynamicFiles[key]
	if shared == nil {
		shared = &dynamicSharedFile{
			record:    record,
			byClient:  make(map[int32]SourceEntry),
			completes: make(map[int32]bool),
		}
		s.dynamicFiles[key] = shared
	}
	base := record
	base.Endpoints = nil
	shared.record = mergeDynamicFileRecord(shared.record, base)
	if len(record.Endpoints) > 0 {
		shared.byClient[clientID] = record.Endpoints[0]
	}
	shared.completes[clientID] = record.CompleteSources > 0
}

func (s *Server) removeDynamicLocked(clientID int32, hash protocol.Hash) {
	key := hash.String()
	shared := s.dynamicFiles[key]
	if shared == nil {
		return
	}
	delete(shared.byClient, clientID)
	delete(shared.completes, clientID)
	if len(shared.byClient) == 0 {
		delete(s.dynamicFiles, key)
	}
}

func (d *dynamicSharedFile) materialize() FileRecord {
	record := d.record
	record.Endpoints = record.Endpoints[:0]
	for _, source := range d.byClient {
		record.Endpoints = append(record.Endpoints, source)
	}
	record.Sources = len(record.Endpoints)
	record.CompleteSources = 0
	for _, complete := range d.completes {
		if complete {
			record.CompleteSources++
		}
	}
	if record.CompleteSources == 0 {
		record.CompleteSources = record.Sources
	}
	return record
}

func mergeDynamicFileRecord(dst, src FileRecord) FileRecord {
	if dst.Hash.IsZero() {
		return src
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if dst.Size == 0 {
		dst.Size = src.Size
	}
	if dst.FileType == "" {
		dst.FileType = src.FileType
	}
	if dst.Extension == "" {
		dst.Extension = src.Extension
	}
	if dst.MediaCodec == "" {
		dst.MediaCodec = src.MediaCodec
	}
	if dst.MediaLength == 0 {
		dst.MediaLength = src.MediaLength
	}
	if dst.MediaBitrate == 0 {
		dst.MediaBitrate = src.MediaBitrate
	}
	return dst
}

func sourceFromSharedEntry(clientID int32, listenPort int, entry serverproto.SharedFileEntry) (SourceEntry, bool) {
	ip := entry.ClientID
	port := int(entry.Port)
	if ip == 0 || port == 0 || uint32(ip) == 0xfbfbfbfb || uint32(ip) == 0xfcfcfcfc {
		ip = clientID
		port = listenPort
	}
	if ip == 0 || port == 0 {
		return SourceEntry{}, false
	}
	endpoint := protocol.NewEndpoint(ip, port)
	host := endpoint.String()
	if idx := strings.LastIndex(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return SourceEntry{Host: host, Port: port}, true
}

func (s *Server) persistCatalog() error {
	if err := s.catalog.Save(); err != nil {
		return err
	}
	s.bumpCounter(func(stats *serverCounters) {
		stats.PersistWrites++
	})
	return nil
}

func (s *Server) bumpCounter(fn func(*serverCounters)) {
	s.mu.Lock()
	fn(&s.stats)
	s.mu.Unlock()
}

func (c *clientSession) send(typeName string, packet protocol.Serializable) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.sendLocked(typeName, packet)
}

func (c *clientSession) sendLocked(typeName string, packet protocol.Serializable) error {
	raw, err := c.server.combiner.Pack(typeName, packet)
	if err != nil {
		return err
	}
	c.noteOutbound(len(raw))
	c.server.bumpCounter(func(stats *serverCounters) {
		stats.OutboundPackets++
		stats.OutboundBytes += int64(len(raw))
	})
	_, err = c.conn.Write(raw)
	return err
}

// sendFoundSourcesObfuscated 回复 OP_FOUNDSOURCES_OBFU（0x44）：每源在 endpoint 后多 1 字节连接/混淆选项（aMule PartFile::AddSources）。
func (c *clientSession) sendFoundSourcesObfuscated(hash protocol.Hash, sources []protocol.Endpoint) error {
	body := &bytes.Buffer{}
	if err := protocol.WriteHash(body, hash); err != nil {
		return err
	}
	if len(sources) > 255 {
		sources = sources[:255]
	}
	if err := body.WriteByte(byte(len(sources))); err != nil {
		return err
	}
	for _, ep := range sources {
		if err := protocol.WriteInt32(body, ep.IP()); err != nil {
			return err
		}
		if err := binary.Write(body, binary.LittleEndian, uint16(ep.Port())); err != nil {
			return err
		}
		if err := body.WriteByte(0); err != nil {
			return err
		}
	}
	return c.sendRawEd2k(opFoundSourcesObfu, body.Bytes())
}

func (c *clientSession) sendRawEd2k(opcode byte, body []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	var header protocol.PacketHeader
	header.ResetWithKey(protocol.PK(protocol.EdonkeyHeader, opcode), int32(len(body)+1))
	var frame bytes.Buffer
	if err := header.Put(&frame); err != nil {
		return err
	}
	if _, err := frame.Write(body); err != nil {
		return err
	}
	raw := frame.Bytes()
	c.noteOutbound(len(raw))
	c.server.bumpCounter(func(stats *serverCounters) {
		stats.OutboundPackets++
		stats.OutboundBytes += int64(len(raw))
	})
	_, err := c.conn.Write(raw)
	return err
}

func (c *clientSession) noteInbound(frameBytes int) {
	c.mu.Lock()
	c.lastSeenAt = time.Now()
	c.inboundPackets++
	c.inboundBytes += int64(frameBytes)
	c.mu.Unlock()
}

func (c *clientSession) noteOutbound(frameBytes int) {
	c.mu.Lock()
	c.lastSeenAt = time.Now()
	c.outboundPackets++
	c.outboundBytes += int64(frameBytes)
	c.mu.Unlock()
}

func (c *clientSession) noteSearchRequest() {
	c.mu.Lock()
	c.lastSeenAt = time.Now()
	c.searchRequests++
	c.mu.Unlock()
}

func (c *clientSession) noteSourceRequest() {
	c.mu.Lock()
	c.lastSeenAt = time.Now()
	c.sourceRequests++
	c.mu.Unlock()
}

func (c *clientSession) noteCallbackRequest() {
	c.mu.Lock()
	c.lastSeenAt = time.Now()
	c.callbackRequests++
	c.mu.Unlock()
}

func (c *clientSession) snapshot() ClientSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	remoteAddress := ""
	if c.remote != nil {
		remoteAddress = c.remote.String()
	}
	listenEndpoint := ""
	if c.loginPoint.Defined() {
		listenEndpoint = c.loginPoint.String()
	}
	return ClientSnapshot{
		ClientID:         c.assignedID,
		ClientName:       c.clientName,
		ClientHash:       c.clientHash,
		RemoteAddress:    remoteAddress,
		ListenEndpoint:   listenEndpoint,
		ConnectedAt:      c.connectedAt,
		LastSeenAt:       c.lastSeenAt,
		SearchRequests:   c.searchRequests,
		SourceRequests:   c.sourceRequests,
		CallbackRequests: c.callbackRequests,
		InboundPackets:   c.inboundPackets,
		OutboundPackets:  c.outboundPackets,
		InboundBytes:     c.inboundBytes,
		OutboundBytes:    c.outboundBytes,
	}
}

func readFrame(conn net.Conn) (protocol.PacketHeader, []byte, int, error) {
	headerBuf := make([]byte, protocol.PacketHeaderSize)
	if _, err := io.ReadFull(conn, headerBuf); err != nil {
		return protocol.PacketHeader{}, nil, 0, err
	}
	var header protocol.PacketHeader
	if err := header.Get(bytes.NewReader(headerBuf)); err != nil {
		return protocol.PacketHeader{}, nil, 0, err
	}
	bodySize := int(header.SizePacket())
	if bodySize < 0 {
		return protocol.PacketHeader{}, nil, 0, fmt.Errorf("invalid body size: %d", bodySize)
	}
	body := make([]byte, bodySize)
	if _, err := io.ReadFull(conn, body); err != nil {
		return protocol.PacketHeader{}, nil, 0, err
	}
	return header, body, protocol.PacketHeaderSize + bodySize, nil
}

func extractClientName(tags protocol.TagList) string {
	for _, tag := range tags {
		if tag.ID == 0x01 {
			return normalizeDisplayText(tag.String)
		}
	}
	return ""
}

func clientIDFromRemote(addr *net.TCPAddr) int32 {
	if addr == nil || addr.IP == nil {
		return 0
	}
	return protocol.EndpointFromInet(addr).IP()
}
