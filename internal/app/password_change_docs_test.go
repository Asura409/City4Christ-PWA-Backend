package app

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestPasswordChangeOpenAPIContract(t *testing.T) {
	w := httptest.NewRecorder()
	New(nil, Config{}).ServeHTTP(w, httptest.NewRequest("GET", "/openapi.json", nil))
	var spec struct {
		Paths map[string]map[string]struct {
			Security    []map[string][]string `json:"security"`
			RequestBody struct {
				Required bool `json:"required"`
				Content  map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
			Responses map[string]json.RawMessage `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				AdditionalProperties *bool    `json:"additionalProperties"`
				Required             []string `json:"required"`
				Properties           map[string]struct {
					Type      string `json:"type"`
					Format    string `json:"format"`
					WriteOnly bool   `json:"writeOnly"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	op, exists := spec.Paths["/api/v1/auth/password/change"]["post"]
	if !exists {
		t.Fatal("missing signed-in password change operation")
	}
	if len(op.Security) != 1 {
		t.Fatal("password change must require Bearer authorization")
	}
	if _, ok := op.Security[0]["bearerAuth"]; !ok {
		t.Fatal("missing bearerAuth security")
	}
	if !op.RequestBody.Required || op.RequestBody.Content["application/json"].Schema.Ref != "#/components/schemas/PasswordChangeInput" {
		t.Fatal("missing required password change JSON schema")
	}
	schema, ok := spec.Components.Schemas["PasswordChangeInput"]
	if !ok || schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Fatal("password change input must reject unknown fields")
	}
	if len(schema.Required) != 2 || len(schema.Properties) != 2 {
		t.Fatal("password change must require exactly the current and new password")
	}
	for _, field := range []string{"currentPassword", "newPassword"} {
		found := false
		for _, required := range schema.Required {
			if required == field {
				found = true
			}
		}
		p := schema.Properties[field]
		if !found || p.Type != "string" || p.Format != "password" || !p.WriteOnly {
			t.Fatalf("missing protected password field %s", field)
		}
	}
	for _, status := range []string{"200", "400", "401", "403", "409", "422", "429", "503"} {
		if len(op.Responses[status]) == 0 {
			t.Fatalf("missing password change response %s", status)
		}
	}
	var success struct {
		Headers map[string]json.RawMessage `json:"headers"`
		Content map[string]struct {
			Example struct {
				Data struct {
					Status string `json:"status"`
				} `json:"data"`
			} `json:"example"`
		} `json:"content"`
	}
	if err := json.Unmarshal(op.Responses["200"], &success); err != nil {
		t.Fatal(err)
	}
	if len(success.Headers["Set-Cookie"]) == 0 || success.Content["application/json"].Example.Data.Status != "password_changed" {
		t.Fatal("success must document cookie clearing and password_changed status")
	}
}
