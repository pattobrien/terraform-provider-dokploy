package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ahmedali6/terraform-provider-dokploy/internal/client"
)

// newTestResource serves application.one with the given stored application and
// records every mutation payload by endpoint.
func newTestResource(t *testing.T, storedApp map[string]interface{}) (*ApplicationResource, map[string]map[string]interface{}) {
	t.Helper()

	payloads := map[string]map[string]interface{}{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "?", 2)[0]
		if r.Method == http.MethodPost {
			var payload map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decoding %s payload: %v", endpoint, err)
			}
			payloads[endpoint] = payload
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(storedApp)
	}))
	t.Cleanup(server.Close)

	return &ApplicationResource{client: client.NewDokployClient(server.URL, "test")}, payloads
}

// An unset railpack_version must not blank out the version Dokploy stores:
// saveBuildType writes the field verbatim and an empty one makes the build pull
// ghcr.io/railwayapp/railpack-frontend:v.
func TestSaveBuildTypeKeepsStoredVersionsWhenUnset(t *testing.T) {
	r, payloads := newTestResource(t, map[string]interface{}{
		"applicationId":   "app-1",
		"railpackVersion": "0.15.4",
		"herokuVersion":   "24",
	})

	plan := &ApplicationResourceModel{BuildType: types.StringValue("railpack")}
	if err := r.saveBuildType("app-1", plan); err != nil {
		t.Fatalf("saveBuildType: %v", err)
	}

	payload := payloads["application.saveBuildType"]
	if got := payload["railpackVersion"]; got != "0.15.4" {
		t.Errorf("railpackVersion = %q, want 0.15.4", got)
	}
	if got := payload["herokuVersion"]; got != "24" {
		t.Errorf("herokuVersion = %q, want 24", got)
	}
}

func TestSaveBuildTypeSendsConfiguredVersion(t *testing.T) {
	r, payloads := newTestResource(t, map[string]interface{}{
		"applicationId":   "app-1",
		"railpackVersion": "0.15.4",
		"herokuVersion":   "24",
	})

	plan := &ApplicationResourceModel{
		BuildType:       types.StringValue("railpack"),
		RailpackVersion: types.StringValue("0.16.0"),
	}
	if err := r.saveBuildType("app-1", plan); err != nil {
		t.Fatalf("saveBuildType: %v", err)
	}

	if got := payloads["application.saveBuildType"]["railpackVersion"]; got != "0.16.0" {
		t.Errorf("railpackVersion = %q, want 0.16.0", got)
	}
}

func TestSaveSourceProviderSendsWatchPaths(t *testing.T) {
	r, payloads := newTestResource(t, map[string]interface{}{"applicationId": "app-1"})

	watchPaths, diags := types.ListValueFrom(t.Context(), types.StringType, []string{"apps/api"})
	if diags.HasError() {
		t.Fatalf("building watch_paths: %v", diags)
	}
	plan := &ApplicationResourceModel{
		SourceType: types.StringValue("github"),
		WatchPaths: watchPaths,
	}
	if err := r.saveSourceProvider("app-1", plan); err != nil {
		t.Fatalf("saveSourceProvider: %v", err)
	}

	got, ok := payloads["application.saveGithubProvider"]["watchPaths"].([]interface{})
	if !ok || len(got) != 1 || got[0] != "apps/api" {
		t.Errorf("watchPaths = %v, want [apps/api]", payloads["application.saveGithubProvider"]["watchPaths"])
	}
}

func TestSaveSourceProviderOmitsUnsetWatchPaths(t *testing.T) {
	r, payloads := newTestResource(t, map[string]interface{}{"applicationId": "app-1"})

	plan := &ApplicationResourceModel{SourceType: types.StringValue("github")}
	if err := r.saveSourceProvider("app-1", plan); err != nil {
		t.Fatalf("saveSourceProvider: %v", err)
	}

	if _, present := payloads["application.saveGithubProvider"]["watchPaths"]; present {
		t.Error("watchPaths sent for an unset list")
	}
}
