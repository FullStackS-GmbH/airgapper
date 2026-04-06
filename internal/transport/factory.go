// Package transport provides the factory for creating and managing Transporter
// instances. It serves as the central registry for all supported artifact
// transport backends.
package transport

import (
	"fmt"

	"github.com/fullstacks-gmbh/universal-airgapper/internal/domain"
)

// Factory creates and manages Transporter instances. It maps resource types to
// their corresponding transporter implementations and provides lookup by type.
type Factory struct {
	transporters map[domain.ResourceType]domain.Transporter
}

// NewFactory creates a factory with the provided transporters registered. Each
// transporter is keyed by its Type(). If multiple transporters share the same
// type, the last one wins.
func NewFactory(transporters ...domain.Transporter) *Factory {
	f := &Factory{
		transporters: make(map[domain.ResourceType]domain.Transporter, len(transporters)),
	}
	for _, t := range transporters {
		f.transporters[t.Type()] = t
	}
	return f
}

// ForType returns the transporter registered for the given resource type.
// It returns domain.ErrUnsupportedTransport if no transporter is registered
// for that type.
func (f *Factory) ForType(rt domain.ResourceType) (domain.Transporter, error) {
	t, ok := f.transporters[rt]
	if !ok {
		return nil, fmt.Errorf("no transporter for resource type %q: %w", rt, domain.ErrUnsupportedTransport)
	}
	return t, nil
}

// RegisteredTypes returns all registered resource types. The order of the
// returned slice is not guaranteed.
func (f *Factory) RegisteredTypes() []domain.ResourceType {
	types := make([]domain.ResourceType, 0, len(f.transporters))
	for rt := range f.transporters {
		types = append(types, rt)
	}
	return types
}
