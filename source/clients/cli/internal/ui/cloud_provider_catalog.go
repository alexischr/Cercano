package ui

import (
	"fmt"
	"strings"

	"cercano/source/clients/cli/internal/form"
	"cercano/source/server/pkg/agentclient"
)

// The combined inventory is an explicit, read-only view within Mistral's
// provider settings. Account dropdowns remain strictly account-filtered.
func (sp *settingsPage) mistralProviderCatalogFields() []form.Field {
	label := "Show Mistral catalog across accounts"
	if sp.cloudProviderCatalog != nil {
		label = "Hide Mistral catalog across accounts"
	}
	fields := []form.Field{form.NewButton("cloud-provider-catalog", label, true)}
	if sp.cloudProviderCatalog == nil {
		return fields
	}
	for i, status := range sp.cloudProviderCatalog.CatalogAccountStatus {
		if status.Error != "" {
			fields = append(fields, form.NewReadOnly(fmt.Sprintf("cloud-provider-status-%d", i), status.Profile, status.Error, "Catalog is incomplete while account discovery fails"))
		}
	}
	count := 0
	for _, m := range sp.cloudProviderCatalog.Models {
		if m.CatalogSource != "mistral" {
			continue
		}
		details := []string{}
		for _, obs := range m.AccountObservations {
			details = append(details, cloudModelMetadataLabel(agentclient.CloudModelInfo{Metadata: obs.Metadata}, obs.Profile))
		}
		fields = append(fields, form.NewReadOnly(fmt.Sprintf("cloud-provider-model-%d", count), m.CatalogID, strings.Join(details, " | "), "Select using the intended account's model dropdown above"))
		count++
	}
	if count == 0 {
		fields = append(fields, form.NewReadOnly("cloud-provider-empty", "Mistral catalog", "No currently accessible models", "Configure an account and check discovery status"))
	}
	return fields
}
