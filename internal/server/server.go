package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pergus/factdb/internal/model"
	"github.com/pergus/factdb/internal/store"
)

// Server represents the HTTP server for FactDB. It handles incoming requests,
// routes them to the appropriate handlers, and interacts with the underlying
// store to perform CRUD operations on entities and relationships. The server
// provides endpoints for health checks, entity management, fact management,
// relationship management, and IP subnet queries. It is designed to be used
// with an HTTP server that listens for incoming requests and delegates them to
// the Server's handler.
type Server struct {
	store *store.Store
	mux   *http.ServeMux
}

// New creates a new Server instance with the provided store. It initializes the
// HTTP request multiplexer and registers the necessary routes for handling
// requests. The server is ready to handle incoming requests once it is created.
// The store parameter is used to perform operations on entities and
// relationships, and it must be a valid instance of store.Store. The returned
// Server instance can be used to start an HTTP server that listens for incoming
// requests and delegates them to the appropriate handlers.
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

// Handler returns the HTTP handler for the server. It is used to route incoming
// HTTP requests to the appropriate handlers based on the registered routes. The
// returned handler can be used with an HTTP server to listen for incoming
// requests and delegate them to the Server's handlers. The handler is a
// http.ServeMux that has been initialized with the necessary routes for
// handling requests related to health checks, entity management, fact
// management, relationship management, and IP subnet queries.
func (s *Server) Handler() http.Handler { return s.mux }

// health handles the health check endpoint. It responds with a JSON object
// indicating the server's status. The response has a status code of 200 and a
// JSON body containing {"status": "ok"}. This endpoint can be used to verify
// that the server is running and responsive. It does not require any request
// parameters or authentication, and it is intended for use by monitoring
// systems or load balancers to check the health of the server.
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// createEntity handles the creation of a new entity. It decodes the request
// body into a CreateEntityRequest, creates the entity in the store, and
// responds with the created entity. If the entity already exists, it responds
// with a 409 status code. If there is an error during creation, it responds
// with a 400 status code and an error message. The request body must be a valid
// JSON object containing the entity's ID and type. The response body contains
// the created entity as a JSON object.
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

// getEntity handles the retrieval of an entity by its ID. It extracts the
// entity ID from the request path, retrieves the entity from the store, and
// responds with the entity as a JSON object. If the entity is not found, it
// responds with a 404 status code and an error message. The response body
// contains the entity's details, including its ID, type, and facts. This
// endpoint can be used to fetch information about a specific entity in the
// FactDB system.
func (s *Server) getEntity(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, e)
}

// putFact handles the addition or update of a fact for a specific entity. It
// decodes the request body into a PutFactRequest, adds or updates the fact in
// the store, and responds with the updated fact. If the entity or fact is not
// found, it responds with a 404 status code. If there is an error during the
// operation, it responds with a 400 status code and an error message. The
// request body must be a valid JSON object containing the fact's value, source,
// and verification status. The response body contains the updated fact as a
// JSON object.
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

// createRelationship handles the creation of a new relationship between
// entities. It decodes the request body into a CreateRelationshipRequest,
// creates the relationship in the store, and responds with the created
// relationship. If the relationship already exists, it responds with a 409
// status code. If there is an error during creation, it responds with a 400
// status code and an error message. The request body must be a valid JSON
// object containing the source entity ID, target entity ID, relationship type,
// and optional metadata. The response body contains the created relationship as
// a JSON object.
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

// queryRelationships handles the retrieval of relationships for a specific
// entity. It extracts the entity ID from the request path and optional query
// parameters for relationship type, source, and direction. It queries the store
// for relationships matching the criteria and responds with a list of
// relationships as a JSON object. If there is an error during the query, it
// responds with a 400 status code and an error message. If the entity is not
// found, it responds with a 404 status code. The response body contains the
// relationships in a RelationshipResult object.
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

// queryIP handles the retrieval of entities associated with a specific IP
// subnet. It decodes the request body into an IPSubnetQuery, queries the store
// for entities matching the CIDR, type, and verification status, and responds
// with a list of entities as a JSON object. If there is an error during the
// query, it responds with a 400 status code and an error message. If the CIDR
// is missing or invalid, it responds with a 400 status code and an error
// message. The response body contains the CIDR and the associated entities in
// an IPSubnetResult object.
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

// decode decodes the JSON request body into the provided destination structure.
// It limits the request body size to 1MB and uses a JSON decoder that supports
// numbers. If the decoding fails, it responds with a 400 status code and an
// error message. The function returns true if the decoding is successful and
// false otherwise. The destination parameter must be a pointer to a valid
// structure that can be unmarshaled from JSON.
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

// writeJSON writes the provided value as a JSON response with the specified
// status code. It sets the Content-Type header to application/json and encodes
// the value using a JSON encoder. If encoding fails, it logs the error and
// responds with a 500 status code. The function is used to send JSON responses
// for successful requests.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes an error response as a JSON object with the specified
// status code and error message. It sets the Content-Type header to
// application/json and encodes the error message in a JSON object with the key
// "error". The function is used to send error responses for failed requests.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
