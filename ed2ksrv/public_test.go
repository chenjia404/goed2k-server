package ed2ksrv

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicHTTPSearchAndAnnounce(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CatalogPath = filepath.Join("..", "testdata", "catalog.json")
	cfg.AdminListenAddress = ""
	cfg.PublicHTTPEnabled = true
	cfg.PublicHTTPListenAddress = ""
	cfg.PublicSearchBatchSize = 10

	catalog, err := LoadCatalog(cfg.CatalogPath)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	server, err := NewServer(cfg, catalog, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	publicListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen public: %v", err)
	}
	defer publicListener.Close()

	go func() { _ = server.ServePublic(publicListener) }()
	defer shutdownServer(t, server)

	baseURL := "http://" + publicListener.Addr().String()

	t.Run("healthz", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/v1/healthz")
		if err != nil {
			t.Fatalf("get healthz: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("healthz status: %d", resp.StatusCode)
		}
	})

	t.Run("search ubuntu", func(t *testing.T) {
		var items []FileRecord
		meta := getJSON(t, newGETRequest(t, baseURL+"/api/v1/search?q=ubuntu&sort=name"), &items)
		if len(items) != 1 {
			t.Fatalf("expected 1 result, got %d", len(items))
		}
		if !strings.Contains(strings.ToLower(items[0].Name), "ubuntu") {
			t.Fatalf("unexpected result: %+v", items[0])
		}
		if meta["total"] != float64(1) {
			t.Fatalf("unexpected total: %v", meta["total"])
		}
	})

	t.Run("file detail and sources", func(t *testing.T) {
		hash := "31D6CFE0D16AE931B73C59D7E0C089C0"
		var record FileRecord
		getJSON(t, newGETRequest(t, baseURL+"/api/v1/files/"+hash), &record)
		if record.Name == "" {
			t.Fatalf("missing file name")
		}

		var sources PublicSourcesResponse
		getJSON(t, newGETRequest(t, baseURL+"/api/v1/files/"+hash+"/sources"), &sources)
		if sources.Sources < 2 {
			t.Fatalf("expected at least 2 sources, got %d", sources.Sources)
		}
	})

	t.Run("announce and scrape", func(t *testing.T) {
		hash := "31D6CFE0D16AE931B73C59D7E0C089C0"
		payload := map[string]any{
			"hash":   hash,
			"host":   "203.0.113.10",
			"port":   4662,
			"left":   0,
			"event":  "started",
			"peer_id": "test-peer-001",
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal announce: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/announce", strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("announce: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("announce status: %d", resp.StatusCode)
		}

		var announce announceResponse
		decodeAPI(t, resp, &announce)
		if announce.Complete < 1 {
			t.Fatalf("expected complete peers after announce")
		}

		var scrape struct {
			Files map[string]map[string]int `json:"files"`
		}
		getJSON(t, newGETRequest(t, baseURL+"/api/v1/scrape?hash="+hash), &scrape)
		if scrape.Files[hash]["complete"] < 1 {
			t.Fatalf("scrape complete count too low: %+v", scrape.Files)
		}
	})

	t.Run("public ui", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/")
		if err != nil {
			t.Fatalf("get ui: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("ui status: %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read ui: %v", err)
		}
		if !strings.Contains(string(body), "ED2K") {
			t.Fatalf("ui missing title")
		}
	})
}

func TestPeerStoreUpsertAndExpire(t *testing.T) {
	store := NewPeerStore(50 * 1e9) // very long timeout
	store.Upsert("HASH", HTTPPeer{PeerID: "p1", Host: "1.2.3.4", Port: 4662, Left: 0})
	peers := store.Peers("HASH")
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	if !store.Remove("HASH", HTTPPeer{PeerID: "p1", Host: "1.2.3.4", Port: 4662}) {
		t.Fatalf("expected remove to succeed")
	}
	if len(store.Peers("HASH")) != 0 {
		t.Fatalf("expected empty peers after remove")
	}
}

func newGETRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new GET request: %v", err)
	}
	return req
}

func mustGET(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("GET %s status: %d", url, resp.StatusCode)
	}
	return resp
}

func decodeAPI(t *testing.T, resp *http.Response, target any) {
	t.Helper()
	defer resp.Body.Close()
	var body apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode api: %v", err)
	}
	if !body.OK {
		t.Fatalf("api error: %s", body.Err)
	}
	raw, err := json.Marshal(body.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
}
