package model

import "time"

// Fact represents a single piece of information about an entity, along with its
// source and verification status. The Value field can hold any type of data,
// while the Source field indicates where the fact was obtained from. The
// ObservedAt field records when the fact was observed, and the Verified field
// indicates whether the fact has been confirmed to be accurate.
type Fact struct {
	Value      any       `json:"value"`
	Source     string    `json:"source,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
	Verified   bool      `json:"verified"`
}

// Relationship is stored once, independently of either endpoint entity. The ID
// field is a unique identifier for the relationship, which can be used to
// reference or manage the relationship in the system. FromID and ToID give the
// relationship its semantic direction. The Type field describes the nature of
// the relationship, while the Source field indicates where the relationship
// information was obtained from. The CreatedAt field records when the
// relationship was created in the system.
type Relationship struct {
	ID        string    `json:"id"`
	FromID    string    `json:"fromId"`
	Type      string    `json:"type"`
	ToID      string    `json:"toId"`
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Entity represents a distinct object in the system, identified by a unique ID
// and a type. It contains a collection of facts that describe the entity, along
// with timestamps for when the entity was created and last updated. The Facts
// field is a map where each key is a fact name and the value is a Fact object,
// allowing for flexible representation of various attributes associated with
// the entity. The CreatedAt and UpdatedAt fields help track the lifecycle of
// the entity within the system.
type Entity struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Facts     map[string]Fact `json:"facts"`
}

// Snapshot represents the entire state of the system at a given point in time,
// including all entities and relationships. The Entities field is a map where
// each key is an entity ID and the value is a pointer to an Entity object,
// allowing for efficient access and management of entities. The
// Relationships field is a map where each key is a relationship ID and the
// value is a Relationship object, enabling the system to maintain and query
// relationships between entities. This structure allows for easy serialization
// and deserialization of the system's state, facilitating persistence and
// recovery.
type Snapshot struct {
	Entities      map[string]*Entity      `json:"entities"`
	Relationships map[string]Relationship `json:"relationships"`
}

// CreateEntityRequest is used to create a new entity in the system. It requires
// an ID and a type for the entity. The ID must be unique within the system, and
// the type describes the category or classification of the entity. This
// structure is typically used in API requests to add new entities to the
// system, ensuring that all necessary information is provided for successful
// creation.
type CreateEntityRequest struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// PutFactRequest is used to add or update a fact for an existing entity. It
// includes the value of the fact, the source from which the fact was obtained,
// and a verification status indicating whether the fact has been confirmed to
// be accurate. This structure is typically used in API requests to manage facts
// associated with entities, allowing for flexible and dynamic representation of
// entity attributes.
type PutFactRequest struct {
	Value    any    `json:"value"`
	Source   string `json:"source"`
	Verified bool   `json:"verified"`
}

// CreateRelationshipRequest is used to create a new relationship between two
// entities in the system. It requires the IDs of the source and target
// entities, as well as the type of the relationship. The ID field is optional
// and can be used to specify a unique identifier for the relationship; if not
// provided, the system will generate one. The Source field indicates where the
// relationship information was obtained from. This structure is typically used
// in API requests to establish connections between entities, enabling complex
// data modeling and querying capabilities.
type CreateRelationshipRequest struct {
	ID     string `json:"id,omitempty"`
	FromID string `json:"fromId"`
	Type   string `json:"type"`
	ToID   string `json:"toId"`
	Source string `json:"source,omitempty"`
}

// IPSubnetQuery is used to query entities based on their associated IP
// addresses It includes a CIDR notation string to specify the IP subnet, an
// optional type filter to narrow down the results, and a flag to indicate
// whether only verified IP facts should be considered. This structure is
// typically used in API requests to retrieve entities that have IP addresses
// falling within a specified subnet, allowing for targeted searches and
// analysis of networked entities.
type IPSubnetQuery struct {
	CIDR            string `json:"cidr"`
	Type            string `json:"type,omitempty"`
	RequireVerified bool   `json:"requireVerified,omitempty"`
}

// IPSubnetResult represents the result of an IP subnet query, containing the
// CIDR notation string used for the query and a list of entities that have IP
// addresses falling within the specified subnet. This structure is typically
// returned in API responses to provide clients with the relevant entities
// matching their subnet query, enabling further analysis or processing of the
// retrieved data.
type IPSubnetResult struct {
	CIDR     string   `json:"cidr"`
	Entities []Entity `json:"entities"`
}

// RelationshipQuery is used to query relationships associated with a specific
// entity. It includes the ID of the entity for which relationships are being
// queried, an optional type filter to narrow down the results, and a direction
// field to specify whether to retrieve outgoing, incoming, or both types of
// relationships. This structure is typically used in API requests to retrieve
// relationships connected to a particular entity, allowing for exploration of
// the entity's connections within the system.
type RelationshipQuery struct {
	EntityID  string `json:"entityId"`
	Type      string `json:"type,omitempty"`
	Direction string `json:"direction,omitempty"` // outgoing, incoming, or both
}

// RelationshipResult represents the result of a relationship query, containing
// a list of relationships that match the query criteria. This structure is
// typically returned in API responses to provide clients with the relevant
// relationships matching their query, enabling further analysis or processing
// of the retrieved data.
type RelationshipResult struct {
	Relationships []Relationship `json:"relationships"`
}
