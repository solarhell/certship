// Package management stores only local intent, issued material and retry state.
package management

import (
	"context"
	"github.com/solarhell/certship/internal/discovery"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	"github.com/solarhell/certship/pkg/ent/domain"
)

// Load joins cloud inventory with durable local state. Disappeared resources are
// absent from the result. Their local settings/material remain reusable if the
// domain returns; no separate offline/confirmation/archive lifecycle is needed.
func Load(ctx context.Context, snapshot discovery.Result) (map[string]*ent.Domain, error) {
	states := make(map[string]*ent.Domain, len(snapshot.Domains))
	if len(snapshot.Domains) == 0 {
		return states, nil
	}
	names := make([]string, 0, len(snapshot.Domains))
	for _, info := range snapshot.Domains {
		names = append(names, info.Domain)
	}
	db := database.GetClient()
	records, err := db.Domain.Query().Where(domain.DomainIn(names...)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		states[record.Domain] = record
	}
	for _, name := range names {
		if states[name] != nil {
			continue
		}
		record, err := db.Domain.Create().SetDomain(name).Save(ctx)
		if ent.IsConstraintError(err) {
			record, err = db.Domain.Query().Where(domain.DomainEQ(name)).Only(ctx)
		}
		if err != nil {
			return nil, err
		}
		states[name] = record
	}
	return states, nil
}
