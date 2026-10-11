package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pergus/factdb/internal/model"
	"github.com/pergus/factdb/internal/store"
)

type Server struct {
	store *store.Store
	mux   *http.ServeMux
}

func New(st *store.Store) *Server {
	s := &Server{store: st, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("POST /v1/entities", s.createEntity)
	s.mux.HandleFunc("GET /v1/entities/{id}", s.getEntity)
	s.mux.HandleFunc("PUT /v1/entities/{id}/facts/{key}", s.putFact)
	s.mux.HandleFunc("POST /v1/relationships", s.createRelationship)
	s.mux.HandleFunc("GET /v1/entities/{id}/relationships", s.queryRelationships)
	s.mux.HandleFunc("POST /v1/query/ip-in-subnet", s.queryIP)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) createEntity(w http.ResponseWriter, r *http.Request) {
	var q model.CreateEntityRequest
	if !decode(w, r, &q) {
		return
	}
	e, err := s.store.Create(q.ID, q.Type)
	if err != nil {
		status := 400
		if errors.Is(err, store.ErrExists) {
			status = 409
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, 201, e)
}

func (s *Server) getEntity(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, e)
}

func (s *Server) putFact(w http.ResponseWriter, r *http.Request) {
	var q model.PutFactRequest
	if !decode(w, r, &q) {
		return
	}
	e, err := s.store.PutFact(r.PathValue("id"), r.PathValue("key"), q.Value, q.Source, q.Verified)
	if err != nil {
		status := 400
		if errors.Is(err, store.ErrNotFound) {
			status = 404
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, 200, e)
}

func (s *Server) createRelationship(w http.ResponseWriter, r *http.Request) {
	var q model.CreateRelationshipRequest
	if !decode(w, r, &q) {
		return
	}
	rel, err := s.store.CreateRelationship(q)
	if err != nil {
		status := 400
		if errors.Is(err, store.ErrExists) {
			status = 409
		}
		if errors.Is(err, store.ErrNotFound) {
			status = 404
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, 201, rel)
}

func (s *Server) queryRelationships(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	typ := r.URL.Query().Get("type")
	source := r.URL.Query().Get("source")
	direction := r.URL.Query().Get("direction")

	rels, err := s.store.QueryRelationships(id, typ, source, direction)
	if err != nil {
		status := 400
		if errors.Is(err, store.ErrNotFound) {
			status = 404
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, 200, model.RelationshipResult{Relationships: rels})
}

func (s *Server) queryIP(w http.ResponseWriter, r *http.Request) {
	var q model.IPSubnetQuery
	if !decode(w, r, &q) {
		return
	}
	if strings.TrimSpace(q.CIDR) == "" {
		writeError(w, 400, "cidr is required")
		return
	}
	entities, err := s.store.QueryIPSubnet(q.CIDR, q.Type, q.RequireVerified)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, model.IPSubnetResult{CIDR: q.CIDR, Entities: entities})
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		writeError(w, 400, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

var _ = fmt.Sprintf
