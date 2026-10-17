package ui

import "cercano/source/clients/cli/internal/form"

// The shared catalog opens read-only across accounts; choices remain scoped.
func (sp *settingsPage) mistralProviderCatalogFields() []form.Field {
	return []form.Field{form.NewButton("cloud-provider-catalog", "Browse Mistral models across accounts…", true)}
}
