package model

import "time"

type Fact struct {
	Value      any       `json:"value"`
	Source     string    `json:"source,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
	Verified   bool      `json:"verified"`
}

// Relationship is stored once, independently of either endpoint entity.
// FromID and ToID give the relationship its semantic direction.
type Relationship struct {
	ID        string    `json:"id"`
	FromID    string    `json:"fromId"`
	Type      string    `json:"type"`
	ToID      string    `json:"toId"`
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type Entity struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Facts     map[string]Fact `json:"facts"`
}

type Snapshot struct {
	Entities      map[string]*Entity      `json:"entities"`
	Relationships map[string]Relationship `json:"relationships"`
}

type CreateEntityRequest struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type PutFactRequest struct {
	Value    any    `json:"value"`
	Source   string `json:"source"`
	Verified bool   `json:"verified"`
}

type CreateRelationshipRequest struct {
	ID     string `json:"id,omitempty"`
	FromID string `json:"fromId"`
	Type   string `json:"type"`
	ToID   string `json:"toId"`
	Source string `json:"source,omitempty"`
}

type IPSubnetQuery struct {
	CIDR            string `json:"cidr"`
	Type            string `json:"type,omitempty"`
	RequireVerified bool   `json:"requireVerified,omitempty"`
}

type IPSubnetResult struct {
	CIDR     string   `json:"cidr"`
	Entities []Entity `json:"entities"`
}

type RelationshipQuery struct {
	EntityID  string `json:"entityId"`
	Type      string `json:"type,omitempty"`
	Direction string `json:"direction,omitempty"` // outgoing, incoming, or both
}

type RelationshipResult struct {
	Relationships []Relationship `json:"relationships"`
}
