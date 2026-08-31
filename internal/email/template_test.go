package email_test

import (
	"strings"
	"testing"

	"ginvoice/internal/email"
	"ginvoice/internal/store"
)

func TestRenderTemplate_CompanyAddressVars(t *testing.T) {
	data := email.TemplateDataFor(store.Invoice{Number: "INV-1"}, store.Company{
		AddressLine1: "Hauptstr. 1", PostalCode: "10115", City: "Berlin", Country: "Germany",
	})
	got := email.RenderTemplate("{{companyAddressLine1}}, {{companyPostalCode}} {{companyCity}}, {{companyCountry}}", data)
	if got != "Hauptstr. 1, 10115 Berlin, Germany" {
		t.Errorf("got %q", got)
	}
}

func TestRenderTemplate_DefaultInvoiceNotes(t *testing.T) {
	data := email.TemplateDataFor(store.Invoice{}, store.Company{City: "Berlin", Country: "Germany"})
	got := email.RenderTemplate(store.DefaultInvoiceNotes, data)
	if !strings.Contains(got, "Place of issue: Berlin,Germany,") {
		t.Errorf("default notes not rendered with city/country: %q", got)
	}
}
