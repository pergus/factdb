package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pergus/factdb/internal/model"
)

var (
	ErrNotFound = errors.New("entity not found")
	ErrExists   = errors.New("entity or relationship already exists")
)

type Store struct {
	mu            sync.RWMutex
	path          string
	entities      map[string]*model.Entity
	relationships map[string]model.Relationship
	// Indexes contain relationship IDs, not duplicate relationship records.
	outgoing map[string]map[string]struct{}     // from entity ID -> relationship IDs
	incoming map[string]map[string]struct{}     // to entity ID -> relationship IDs
	ipIndex  map[netip.Addr]map[string]struct{} // normalized IP -> entity IDs
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, entities: map[string]*model.Entity{}, relationships: map[string]model.Relationship{}, outgoing: map[string]map[string]struct{}{}, incoming: map[string]map[string]struct{}{}, ipIndex: map[netip.Addr]map[string]struct{}{}}
	b, err := os.ReadFile(path)
	if err == nil {
		var snap model.Snapshot
		if err := json.Unmarshal(b, &snap); err != nil {
			return nil, fmt.Errorf("decode snapshot: %w", err)
		}
		if snap.Entities != nil {
			s.entities = snap.Entities
		}
		if snap.Relationships != nil {
			s.relationships = snap.Relationships
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	s.rebuildIndexesLocked()
	return s, nil
}

func (s *Store) Create(id, typ string) (model.Entity, error) {
	id, typ = strings.TrimSpace(id), strings.TrimSpace(typ)
	if id == "" || typ == "" {
		return model.Entity{}, errors.New("id and type are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entities[id]; ok {
		return model.Entity{}, ErrExists
	}
	now := time.Now().UTC()
	e := &model.Entity{ID: id, Type: typ, CreatedAt: now, UpdatedAt: now, Facts: map[string]model.Fact{}}
	s.entities[id] = e
	if err := s.persistLocked(); err != nil {
		delete(s.entities, id)
		return model.Entity{}, err
	}
	return cloneEntity(e), nil
}

func (s *Store) Get(id string) (model.Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entities[id]
	if !ok {
		return model.Entity{}, ErrNotFound
	}
	return cloneEntity(e), nil
}

func (s *Store) PutFact(id, key string, value any, source string, verified bool) (model.Entity, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return model.Entity{}, errors.New("fact key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entities[id]
	if !ok {
		return model.Entity{}, ErrNotFound
	}
	before := cloneEntity(e)
	s.removeEntityFromIPIndexLocked(e)
	e.Facts[key] = model.Fact{Value: value, Source: source, ObservedAt: time.Now().UTC(), Verified: verified}
	e.UpdatedAt = time.Now().UTC()
	s.addEntityToIPIndexLocked(e)
	if err := s.persistLocked(); err != nil {
		s.entities[id] = entityPtr(before)
		s.rebuildIndexesLocked()
		return model.Entity{}, err
	}
	return cloneEntity(e), nil
}

func (s *Store) CreateRelationship(req model.CreateRelationshipRequest) (model.Relationship, error) {
	req.ID = strings.TrimSpace(req.ID)
	req.FromID = strings.TrimSpace(req.FromID)
	req.ToID = strings.TrimSpace(req.ToID)
	req.Type = strings.TrimSpace(req.Type)
	if req.FromID == "" || req.ToID == "" || req.Type == "" {
		return model.Relationship{}, errors.New("fromId, toId, and type are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entities[req.FromID]; !ok {
		return model.Relationship{}, fmt.Errorf("from entity %q: %w", req.FromID, ErrNotFound)
	}
	if _, ok := s.entities[req.ToID]; !ok {
		return model.Relationship{}, fmt.Errorf("to entity %q: %w", req.ToID, ErrNotFound)
	}
	if req.ID == "" {
		var err error
		req.ID, err = newID()
		if err != nil {
			return model.Relationship{}, err
		}
	}
	if _, ok := s.relationships[req.ID]; ok {
		return model.Relationship{}, ErrExists
	}
	// Avoid duplicate semantic edges, even if a caller supplies a new ID.
	for _, existing := range s.relationships {
		if existing.FromID == req.FromID && existing.Type == req.Type && existing.ToID == req.ToID {
			return existing, nil
		}
	}
	r := model.Relationship{ID: req.ID, FromID: req.FromID, Type: req.Type, ToID: req.ToID, Source: req.Source, CreatedAt: time.Now().UTC()}
	s.relationships[r.ID] = r
	s.addRelationshipIndexesLocked(r)
	if err := s.persistLocked(); err != nil {
		delete(s.relationships, r.ID)
		s.rebuildIndexesLocked()
		return model.Relationship{}, err
	}
	return r, nil
}

/*
func (s *Store) QueryRelationships(entityID, typ, source, direction string) ([]model.Relationship, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.entities[entityID]; !ok {
		return nil, ErrNotFound
	}

	if direction == "" {
		direction = "both"
	}

	if direction != "outgoing" && direction != "incoming" && direction != "both" {
		return nil, errors.New("direction must be outgoing, incoming, or both")
	}

	ids := map[string]struct{}{}
	if direction == "outgoing" || direction == "both" {
		for id := range s.outgoing[entityID] {
			ids[id] = struct{}{}
		}
	}

	if direction == "incoming" || direction == "both" {
		for id := range s.incoming[entityID] {
			ids[id] = struct{}{}
		}
	}

	out := make([]model.Relationship, 0, len(ids))
	for id := range ids {
		r := s.relationships[id]
		if typ == "" || r.Type == typ {
			out = append(out, r)
		}

		if source != "" && r.Source != source {
			continue
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
*/

func (s *Store) QueryRelationships(entityID, typ, source, direction string) ([]model.Relationship, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.entities[entityID]; !ok {
		return nil, ErrNotFound
	}

	if direction == "" {
		direction = "both"
	}

	if direction != "outgoing" && direction != "incoming" && direction != "both" {
		return nil, errors.New("direction must be outgoing, incoming, or both")
	}

	ids := map[string]struct{}{}

	if direction == "outgoing" || direction == "both" {
		for id := range s.outgoing[entityID] {
			ids[id] = struct{}{}
		}
	}

	if direction == "incoming" || direction == "both" {
		for id := range s.incoming[entityID] {
			ids[id] = struct{}{}
		}
	}

	out := make([]model.Relationship, 0, len(ids))

	for id := range ids {
		r := s.relationships[id]

		if typ != "" && r.Type != typ {
			continue
		}

		if source != "" && r.Source != source {
			continue
		}

		out = append(out, r)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})

	return out, nil
}

func (s *Store) QueryIPSubnet(cidr, typ string, requireVerified bool) ([]model.Entity, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	prefix = prefix.Masked()
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := map[string]struct{}{}
	// This scans distinct indexed IPs, not all entities. A radix trie is a future optimization.
	for ip, entityIDs := range s.ipIndex {
		if !prefix.Contains(ip) {
			continue
		}
		for id := range entityIDs {
			e := s.entities[id]
			if e == nil || (typ != "" && e.Type != typ) {
				continue
			}
			if requireVerified && !entityHasVerifiedIPInPrefix(e, prefix) {
				continue
			}
			ids[id] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	out := make([]model.Entity, 0, len(ordered))
	for _, id := range ordered {
		out = append(out, cloneEntity(s.entities[id]))
	}
	return out, nil
}

func (s *Store) rebuildIndexesLocked() {
	s.outgoing = map[string]map[string]struct{}{}
	s.incoming = map[string]map[string]struct{}{}
	s.ipIndex = map[netip.Addr]map[string]struct{}{}
	for _, r := range s.relationships {
		s.addRelationshipIndexesLocked(r)
	}
	for _, e := range s.entities {
		s.addEntityToIPIndexLocked(e)
	}
}

func (s *Store) addRelationshipIndexesLocked(r model.Relationship) {
	if s.outgoing[r.FromID] == nil {
		s.outgoing[r.FromID] = map[string]struct{}{}
	}
	s.outgoing[r.FromID][r.ID] = struct{}{}
	if s.incoming[r.ToID] == nil {
		s.incoming[r.ToID] = map[string]struct{}{}
	}
	s.incoming[r.ToID][r.ID] = struct{}{}
}

func (s *Store) addEntityToIPIndexLocked(e *model.Entity) {
	for key, f := range e.Facts {
		if key != "ipAddress" && key != "ipAddresses" {
			continue
		}
		for _, ip := range parseIPs(f.Value) {
			if s.ipIndex[ip] == nil {
				s.ipIndex[ip] = map[string]struct{}{}
			}
			s.ipIndex[ip][e.ID] = struct{}{}
		}
	}
}

func (s *Store) removeEntityFromIPIndexLocked(e *model.Entity) {
	for key, f := range e.Facts {
		if key != "ipAddress" && key != "ipAddresses" {
			continue
		}
		for _, ip := range parseIPs(f.Value) {
			if set := s.ipIndex[ip]; set != nil {
				delete(set, e.ID)
				if len(set) == 0 {
					delete(s.ipIndex, ip)
				}
			}
		}
	}
}

func entityHasVerifiedIPInPrefix(e *model.Entity, prefix netip.Prefix) bool {
	for key, f := range e.Facts {
		if key != "ipAddress" && key != "ipAddresses" || !f.Verified {
			continue
		}
		for _, ip := range parseIPs(f.Value) {
			if prefix.Contains(ip) {
				return true
			}
		}
	}
	return false
}

func parseIPs(value any) []netip.Addr {
	var candidates []string
	switch v := value.(type) {
	case string:
		candidates = append(candidates, v)
	case []any:
		for _, x := range v {
			if str, ok := x.(string); ok {
				candidates = append(candidates, str)
			}
		}
	case []string:
		candidates = append(candidates, v...)
	}
	out := []netip.Addr{}
	for _, raw := range candidates {
		ip, err := netip.ParseAddr(strings.TrimSpace(raw))
		if err == nil {
			out = append(out, ip.Unmap())
		}
	}
	return out
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(model.Snapshot{Entities: s.entities, Relationships: s.relationships}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func cloneEntity(e *model.Entity) model.Entity {
	b, _ := json.Marshal(e)
	var out model.Entity
	_ = json.Unmarshal(b, &out)
	if out.Facts == nil {
		out.Facts = map[string]model.Fact{}
	}
	return out
}

func entityPtr(e model.Entity) *model.Entity { return &e }

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "rel-" + hex.EncodeToString(b[:]), nil
}
