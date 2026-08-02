package ed2ksrv

import (
	"fmt"
	"sync"
	"time"
)

// HTTPPeer is an ephemeral peer registered via the public HTTP announce API.
type HTTPPeer struct {
	PeerID     string    `json:"peer_id,omitempty"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Uploaded   int64     `json:"uploaded"`
	Downloaded int64     `json:"downloaded"`
	Left       int64     `json:"left"`
	RemoteAddr string    `json:"-"`
	LastSeen   time.Time `json:"last_seen"`
}

// PeerStore keeps in-memory HTTP announce peers keyed by file hash.
type PeerStore struct {
	mu      sync.RWMutex
	peers   map[string]map[string]*HTTPPeer
	timeout time.Duration
	stopCh  chan struct{}
}

// NewPeerStore creates a peer store with the given idle timeout.
func NewPeerStore(timeout time.Duration) *PeerStore {
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	return &PeerStore{
		peers:   make(map[string]map[string]*HTTPPeer),
		timeout: timeout,
		stopCh:  make(chan struct{}),
	}
}

// StartCleanup launches a background goroutine that removes expired peers.
func (ps *PeerStore) StartCleanup(interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ps.cleanupExpired()
			case <-ps.stopCh:
				return
			}
		}
	}()
}

// Close stops the cleanup goroutine.
func (ps *PeerStore) Close() {
	select {
	case <-ps.stopCh:
	default:
		close(ps.stopCh)
	}
}

func (ps *PeerStore) cleanupExpired() {
	now := time.Now()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for hash, peers := range ps.peers {
		for key, peer := range peers {
			if now.Sub(peer.LastSeen) > ps.timeout {
				delete(peers, key)
			}
		}
		if len(peers) == 0 {
			delete(ps.peers, hash)
		}
	}
}

func peerStoreKey(peer HTTPPeer) string {
	if peer.PeerID != "" {
		return "id:" + peer.PeerID
	}
	return fmt.Sprintf("ep:%s:%d", peer.Host, peer.Port)
}

// Upsert registers or refreshes a peer for the given file hash.
func (ps *PeerStore) Upsert(hash string, peer HTTPPeer) {
	key := peerStoreKey(peer)
	peer.LastSeen = time.Now()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.peers[hash] == nil {
		ps.peers[hash] = make(map[string]*HTTPPeer)
	}
	stored := peer
	ps.peers[hash][key] = &stored
}

// Remove deletes a peer from the swarm.
func (ps *PeerStore) Remove(hash string, peer HTTPPeer) bool {
	key := peerStoreKey(peer)
	ps.mu.Lock()
	defer ps.mu.Unlock()
	peers, ok := ps.peers[hash]
	if !ok {
		return false
	}
	if _, exists := peers[key]; !exists {
		return false
	}
	delete(peers, key)
	if len(peers) == 0 {
		delete(ps.peers, hash)
	}
	return true
}

// Peers returns all active peers for a file hash.
func (ps *PeerStore) Peers(hash string) []HTTPPeer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	peers := ps.peers[hash]
	if len(peers) == 0 {
		return nil
	}
	out := make([]HTTPPeer, 0, len(peers))
	now := time.Now()
	for _, peer := range peers {
		if now.Sub(peer.LastSeen) > ps.timeout {
			continue
		}
		out = append(out, *peer)
	}
	return out
}

// Stats returns complete and incomplete peer counts for a hash.
func (ps *PeerStore) Stats(hash string) (complete, incomplete int) {
	for _, peer := range ps.Peers(hash) {
		if peer.Left == 0 {
			complete++
		} else {
			incomplete++
		}
	}
	return complete, incomplete
}
