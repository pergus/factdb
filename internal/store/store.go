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

// Errors returned by store operations.
// ErrNotFound is returned when an entity or relationship is not found.
// ErrExists is returned when an entity or relationship already exists.
var (
	ErrNotFound = errors.New("entity not found")
	ErrExists   = errors.New("entity or relationship already exists")
)

// Store represents a persistent storage for entities and relationships. It
// maintains in-memory indexes for efficient queries. The store is safe for
// concurrent use by multiple goroutines. The store is persisted to a JSON
// snapshot file on disk. The store is loaded from the snapshot file on startup,
// and rebuilt in memory. The store is not a database, and does not support
// transactions or complex queries.
type Store struct {
	mu            sync.RWMutex                       // protects all fields below
	path          string                             // snapshot file path
	entities      map[string]*model.Entity           // entity ID -> entity pointer
	relationships map[string]model.Relationship      // relationship ID -> relationship
	outgoing      map[string]map[string]struct{}     // from entity ID -> relationship IDs
	incoming      map[string]map[string]struct{}     // to entity ID -> relationship IDs
	ipIndex       map[netip.Addr]map[string]struct{} // normalized IP -> entity IDs
}

// Open loads a store snapshot from path and rebuilds its in-memory indexes. If
// the snapshot file does not exist, an empty store is created. If the snapshot
// file is invalid, an error is returned. The store is safe for concurrent use.
// The store is persisted to the snapshot file on every write operation.
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

// Create adds an entity with the supplied ID and type, then persists the
// change. If the entity already exists, ErrExists is returned. If the ID or
// type is empty, an error is returned. The entity's CreatedAt and UpdatedAt
// timestamps are set to the current time. The returned entity is a detached
// copy.
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

// Get returns a detached copy of the entity with the given ID. If the entity
// does not exist, ErrNotFound is returned. The returned entity is a detached
// copy, so callers cannot mutate the stored entity. The returned entity's facts
// are also detached copies, so callers cannot mutate the stored facts.
func (s *Store) Get(id string) (model.Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entities[id]
	if !ok {
		return model.Entity{}, ErrNotFound
	}
	return cloneEntity(e), nil
}

// PutFact sets or replaces an entity fact and updates the IP index when needed.
// If the entity does not exist, ErrNotFound is returned. If the fact key is
// empty, an error is returned. The entity's UpdatedAt timestamp is updated to
// the current time. The returned entity is a detached copy.
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

// CreateRelationship adds a directed edge, or returns the existing matching
// edge. If the from or to entity does not exist, ErrNotFound is returned. If
// the type is empty, an error is returned. If the ID is empty, a new ID is
// generated. If the ID is not empty and already exists, ErrExists is returned.
// The returned relationship is a detached copy.
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

// QueryRelationships returns relationships for the given entity ID, optionally
// filtered by type, source, and direction. If the entity does not exist,
// ErrNotFound is returned. The direction can be "outgoing", "incoming", or
// "both" (default). The returned relationships are detached copies.
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

// QueryIPSubnet returns entities with indexed IP facts contained by cidr. If
// typ is non-empty, only entities of that type are returned. If requireVerified
// is true, only entities with at least one verified IP fact in the subnet are
// returned. The returned entities are detached copies, so callers cannot mutate
// the stored entities or their facts. The returned entities are sorted by ID.
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

// rebuildIndexesLocked recreates all derived indexes from stored entities and
// relationships. This should be called after loading a snapshot or restoring
// from a failed write. The caller must hold the store's write lock. This is a
// private method and should not be called directly by external code. It is used
// internally to ensure that the in-memory indexes are consistent with the
// stored data. It is safe to call this method multiple times, as it clears and
// rebuilds the indexes each time.
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

// addRelationshipIndexesLocked indexes a relationship by both of its endpoints.
// The caller must hold the store's write lock. This is a private method and
// should not be called directly by external code. It is used internally to
// ensure that the in-memory indexes are consistent with the stored data. It is
// safe to call this method multiple times for the same relationship, as it will
// overwrite any existing index entries.
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

// addEntityToIPIndexLocked indexes parseable IPs from the entity's IP facts.
// The caller must hold the store's write lock. This is a private method and
// should not be called directly by external code. It is used internally to
// ensure that the in-memory indexes are consistent with the stored data. It is
// safe to call this method multiple times for the same entity, as it will
// overwrite any existing index entries.
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

// removeEntityFromIPIndexLocked removes the entity's current IP facts from the
// index. The caller must hold the store's write lock. This is a private method
// and should not be called directly by external code. It is used internally to
// ensure that the in-memory indexes are consistent with the stored data. It is
// safe to call this method multiple times for the same entity, as it will
// remove any existing index entries.
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

// entityHasVerifiedIPInPrefix reports whether a verified IP fact falls within
// prefix. The caller must hold the store's read lock. This is a private method
// and should not be called directly by external code. It is used internally to
// ensure that the in-memory indexes are consistent with the stored data. It is
// safe to call this method multiple times for the same entity, as it will not
// modify any state. The function iterates over the entity's facts, checking for
// keys "ipAddress" or "ipAddresses" that are verified. It then parses the IPs
// and checks if any of them fall within the specified prefix. If a matching
// verified IP is found, it returns true; otherwise, it returns false.
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

// parseIPs extracts valid IP addresses from a string or an array of strings. It
// returns a slice of normalized netip.Addr values. Invalid IPs are ignored. The
// function handles different input types: a single string, a slice of any type,
// or a slice of strings. It trims whitespace from each candidate string and
// attempts to parse it as an IP address. If successful, the normalized address
// is added to the output slice. The function is safe to call with any value
// type, and it will return an empty slice if no valid IPs are found.
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

// persistLocked writes the current entities and relationships to the snapshot
// file. The caller must hold the store's write lock. This is a private method
// and should not be called directly by external code. It is used internally to
// ensure that the in-memory state is persisted to disk. The function marshals
// the entities and relationships into a JSON snapshot, writes it to a temporary
// file, and then renames the temporary file to the snapshot path. This ensures
// that the snapshot is updated atomically, preventing partial writes. If the
// store's path is empty, indicating that persistence is disabled, the function
// returns nil without writing to disk.
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

// cloneEntity returns a deep copy so callers cannot mutate the stored entity.
// The returned entity's facts are also deep copies, so callers cannot mutate
// the stored facts. This is a private method and should not be called directly
// by external code. It is used internally to ensure that the returned entity is
// a detached copy, preventing external code from modifying the store's internal
// state. The function marshals the entity to JSON and then unmarshals it back
// into a new entity value. This effectively creates a deep copy of the entity
// and its facts. If the entity's facts are nil, they are initialized to an
// empty map to ensure that the returned entity has a valid facts map.
func cloneEntity(e *model.Entity) model.Entity {
	b, _ := json.Marshal(e)
	var out model.Entity
	_ = json.Unmarshal(b, &out)
	if out.Facts == nil {
		out.Facts = map[string]model.Fact{}
	}
	return out
}

// entityPtr returns a pointer to an entity value for restoring the entity map.
// This is a private method and should not be called directly by external code.
// It is used internally to convert a detached entity value into a pointer for
// storage in the entities map. The function takes an entity value as input and
// returns a pointer to that value. This allows the store to maintain a map of
// entity IDs to entity pointers, enabling efficient access and modification of
// entities in memory. The function is safe to call with any entity value, and
// it does not modify the input entity. It simply returns a pointer to the
// provided entity value, allowing the store to manage its internal state
// without exposing mutable references to external code.
func entityPtr(e model.Entity) *model.Entity { return &e }

// newID creates a relationship ID from 128 bits of cryptographic randomness.
// The ID is prefixed with "rel-" to indicate that it represents a relationship.
// This is a private method and should not be called directly by external code.
// It is used internally to generate unique IDs for relationships when the
// caller does not provide an ID. The function reads 16 random bytes from the
// crypto/rand package, encodes them as a hexadecimal string, and returns the
// resulting ID. If there is an error reading random bytes, the function returns
// an empty string and the error. The generated ID is suitable for use as a
// unique identifier for relationships in the store.
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "rel-" + hex.EncodeToString(b[:]), nil
}
