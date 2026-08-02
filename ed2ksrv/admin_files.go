package ed2ksrv

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/monkeyWie/goed2k/protocol"
)

var errClientNotFound = errors.New("client not found")

const (
	FileSourceStatic  = "static"
	FileSourceDynamic = "dynamic"
)

// AdminFileRecord extends FileRecord with catalog source metadata for the admin API.
type AdminFileRecord struct {
	FileRecord
	Source            string  `json:"source"`
	OfferingClientIDs []int32 `json:"offering_client_ids,omitempty"`
}

// FilesAdminSnapshot returns all shared files with static/dynamic source labels.
func (s *Server) FilesAdminSnapshot() []AdminFileRecord {
	files := make([]AdminFileRecord, 0, s.catalog.Count())
	for _, record := range cloneFiles(s.catalog.Snapshot()) {
		files = append(files, AdminFileRecord{FileRecord: record, Source: FileSourceStatic})
	}
	s.mu.RLock()
	for _, shared := range s.dynamicFiles {
		files = append(files, shared.adminMaterialize())
	}
	s.mu.RUnlock()
	return files
}

// FileAdminSnapshot returns one shared file with source metadata.
func (s *Server) FileAdminSnapshot(hash protocol.Hash) (AdminFileRecord, bool) {
	if record, ok := s.catalog.Get(hash); ok {
		return AdminFileRecord{FileRecord: record, Source: FileSourceStatic}, true
	}
	s.mu.RLock()
	shared, ok := s.dynamicFiles[hash.String()]
	s.mu.RUnlock()
	if !ok {
		return AdminFileRecord{}, false
	}
	return shared.adminMaterialize(), true
}

func (s *Server) isDynamicOnlyFile(hash protocol.Hash) bool {
	if _, ok := s.catalog.Get(hash); ok {
		return false
	}
	s.mu.RLock()
	_, ok := s.dynamicFiles[hash.String()]
	s.mu.RUnlock()
	return ok
}

// RevokeClientOfferedFiles removes dynamically shared files offered by a connected client.
func (s *Server) RevokeClientOfferedFiles(clientID int32) (int, error) {
	client := s.findClient(clientID)
	if client == nil {
		return 0, errClientNotFound
	}
	client.mu.Lock()
	count := len(client.offeredFiles)
	client.mu.Unlock()
	if count == 0 {
		return 0, nil
	}
	s.removeClientOfferedFiles(clientID)
	return count, nil
}

func (d *dynamicSharedFile) adminMaterialize() AdminFileRecord {
	record := d.materialize()
	ids := make([]int32, 0, len(d.byClient))
	for id := range d.byClient {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return AdminFileRecord{
		FileRecord:        record,
		Source:            FileSourceDynamic,
		OfferingClientIDs: ids,
	}
}

func filterAdminFiles(files []AdminFileRecord, r *http.Request) []AdminFileRecord {
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	fileType := strings.TrimSpace(r.URL.Query().Get("file_type"))
	ext := strings.TrimSpace(r.URL.Query().Get("extension"))
	source := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")))
	out := make([]AdminFileRecord, 0, len(files))
	for _, file := range files {
		if search != "" &&
			!strings.Contains(strings.ToLower(file.Name), search) &&
			!strings.Contains(strings.ToLower(file.Hash.String()), search) {
			continue
		}
		if fileType != "" && !strings.EqualFold(file.FileType, fileType) {
			continue
		}
		if ext != "" && !strings.EqualFold(file.Extension, ext) {
			continue
		}
		if source != "" && file.Source != source {
			continue
		}
		out = append(out, file)
	}
	return out
}

func sortAdminFiles(files []AdminFileRecord, field string) {
	switch field {
	case "size":
		sort.Slice(files, func(i, j int) bool { return files[i].Size > files[j].Size })
	case "sources":
		sort.Slice(files, func(i, j int) bool { return files[i].Sources > files[j].Sources })
	case "source":
		sort.Slice(files, func(i, j int) bool { return files[i].Source < files[j].Source })
	case "name":
		fallthrough
	default:
		sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
	}
}

func paginateAdminFiles(files []AdminFileRecord, r *http.Request) ([]AdminFileRecord, map[string]any) {
	page, perPage := parsePagination(r)
	start, end := bounds(len(files), page, perPage)
	items := []AdminFileRecord{}
	if start < len(files) {
		items = files[start:end]
	}
	return items, pageMeta(page, perPage, len(files), len(items))
}
