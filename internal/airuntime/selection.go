package airuntime

import (
	"context"
	"strings"
)

func NormalizeSelection(selection RuntimeSelection) RuntimeSelection {
	mode := strings.TrimSpace(selection.Mode)
	if mode == "" {
		mode = SelectionInherit
	}
	out := RuntimeSelection{
		Mode:      mode,
		ProfileID: strings.TrimSpace(selection.ProfileID),
		CLIID:     strings.TrimSpace(selection.CLIID),
		ModelID:   strings.TrimSpace(selection.ModelID),
	}
	if selection.Parameters != nil {
		out.Parameters = cloneMap(selection.Parameters)
	}
	return out
}

func SelectionIsZero(selection RuntimeSelection) bool {
	return strings.TrimSpace(selection.Mode) == "" &&
		strings.TrimSpace(selection.ProfileID) == "" &&
		strings.TrimSpace(selection.CLIID) == "" &&
		strings.TrimSpace(selection.ModelID) == "" &&
		len(selection.Parameters) == 0
}

func (r *RuntimeResolver) ValidateCatalogSelection(catalog Catalog, selection RuntimeSelection) (RuntimeSelectionValidation, error) {
	normalized := NormalizeSelection(selection)
	snapshot, err := r.ResolveCatalog(catalog, normalized)
	if err != nil {
		return RuntimeSelectionValidation{}, err
	}
	return RuntimeSelectionValidation{Selection: normalized, Snapshot: snapshot}, nil
}

func (s *Service) ValidateSelection(ctx context.Context, orgID string, selection RuntimeSelection) (RuntimeSelectionValidation, error) {
	catalog, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return RuntimeSelectionValidation{}, err
	}
	resolver := NewRuntimeResolver(s.repo)
	resolver.now = s.now
	return resolver.ValidateCatalogSelection(catalog, selection)
}
