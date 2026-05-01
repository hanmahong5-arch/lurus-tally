// Package horticulture contains domain entities for the nursery/horticulture vertical.
package horticulture

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NurseryType classifies a plant species.
type NurseryType string

const (
	NurseryTypeTree    NurseryType = "tree"
	NurseryTypeShrub   NurseryType = "shrub"
	NurseryTypeHerb    NurseryType = "herb"
	NurseryTypeVine    NurseryType = "vine"
	NurseryTypeBamboo  NurseryType = "bamboo"
	NurseryTypeAquatic NurseryType = "aquatic"
	NurseryTypeBulb    NurseryType = "bulb"
	NurseryTypeFruit   NurseryType = "fruit"
)

// String returns the string representation of the NurseryType.
func (t NurseryType) String() string {
	return string(t)
}

// ParseNurseryType converts a raw string to NurseryType with validation.
func ParseNurseryType(s string) (NurseryType, error) {
	switch NurseryType(s) {
	case NurseryTypeTree, NurseryTypeShrub, NurseryTypeHerb, NurseryTypeVine,
		NurseryTypeBamboo, NurseryTypeAquatic, NurseryTypeBulb, NurseryTypeFruit:
		return NurseryType(s), nil
	default:
		return "", fmt.Errorf("invalid nursery type: %q", s)
	}
}

// NurseryDict is the canonical species record in the nursery dictionary.
// tenant_id = uuid.Nil denotes a shared seed entry visible to all tenants.
type NurseryDict struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Name          string
	LatinName     string
	Family        string
	Genus         string
	Type          NurseryType
	IsEvergreen   bool
	ClimateZones  []string
	BestSeason    [2]int          // [start_month, end_month]; [0,0] means unset
	SpecTemplate  json.RawMessage // e.g. {"胸径_cm": null, "冠幅_cm": null}
	DefaultUnitID *uuid.UUID
	PhotoURL      string
	Remark        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
}

// Validate enforces domain invariants.
func (d *NurseryDict) Validate() error {
	if d.Name == "" {
		return errors.New("name is required")
	}
	// BestSeason: [0,0] is the unset sentinel (valid); otherwise both months must be in [1,12].
	s := d.BestSeason
	if s[0] != 0 || s[1] != 0 {
		if s[0] < 1 || s[0] > 12 || s[1] < 1 || s[1] > 12 {
			return errors.New("best_season month must be between 1 and 12")
		}
	}
	if d.Type != "" {
		if _, err := ParseNurseryType(string(d.Type)); err != nil {
			return fmt.Errorf("invalid type: %w", err)
		}
	}
	return nil
}

// CreateInput carries fields for creating a new NurseryDict.
type CreateInput struct {
	TenantID      uuid.UUID
	Name          string
	LatinName     string
	Family        string
	Genus         string
	Type          NurseryType
	IsEvergreen   bool
	ClimateZones  []string
	BestSeason    [2]int
	SpecTemplate  json.RawMessage
	DefaultUnitID *uuid.UUID
	PhotoURL      string
	Remark        string
}

// UpdateInput carries mutable fields (nil pointer = do not update).
type UpdateInput struct {
	Name          *string
	LatinName     *string
	Family        *string
	Genus         *string
	Type          *NurseryType
	IsEvergreen   *bool
	ClimateZones  []string
	BestSeason    *[2]int
	SpecTemplate  json.RawMessage
	DefaultUnitID *uuid.UUID
	PhotoURL      *string
	Remark        *string
}

// ListFilter controls list queries.
type ListFilter struct {
	TenantID    uuid.UUID
	Query       string // ILIKE on name
	Type        *NurseryType
	IsEvergreen *bool
	Limit       int
	Offset      int
}
