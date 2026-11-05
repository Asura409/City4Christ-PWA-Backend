package bible

import "context"

// MultiProvider combines catalogs and routes each translation to its owner.
// This lets open translations remain available alongside an optional licensed
// provider without exposing provider credentials to the frontend.
type MultiProvider struct{ providers []Provider }

func NewMultiProvider(providers ...Provider) *MultiProvider {
	result := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		if provider != nil {
			result = append(result, provider)
		}
	}
	return &MultiProvider{providers: result}
}

func (m *MultiProvider) Translations(ctx context.Context) ([]Translation, error) {
	seen := map[string]bool{}
	result := []Translation{}
	for _, provider := range m.providers {
		catalog, ok := provider.(CatalogProvider)
		if !ok {
			for _, translation := range Translations() {
				if !seen[translation.ID] {
					seen[translation.ID] = true
					result = append(result, translation)
				}
			}
			continue
		}
		translations, err := catalog.Translations(ctx)
		if err != nil {
			continue
		}
		for _, translation := range translations {
			if !seen[translation.ID] {
				seen[translation.ID] = true
				result = append(result, translation)
			}
		}
	}
	if len(result) == 0 {
		return nil, ErrUnavailable
	}
	return result, nil
}

func (m *MultiProvider) Lookup(ctx context.Context, q Query) (Passage, error) {
	for _, provider := range m.providers {
		catalog, ok := provider.(CatalogProvider)
		if !ok {
			if _, supported := findTranslation(q.Translation); supported {
				return provider.Lookup(ctx, q)
			}
			continue
		}
		translations, err := catalog.Translations(ctx)
		if err != nil {
			continue
		}
		for _, translation := range translations {
			if translation.ID == q.Translation {
				return provider.Lookup(ctx, q)
			}
		}
	}
	return Passage{}, invalid("translation", "Choose a supported translation.")
}
