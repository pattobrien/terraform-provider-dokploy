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

// newTraefikTestResource serves application.readTraefikConfig with the given
// file (Dokploy returns it as a JSON string) and records mutation payloads by
// endpoint plus how often the config was read.
func newTraefikTestResource(t *testing.T, storedConfig string) (*ApplicationResource, map[string]map[string]interface{}, *int) {
	t.Helper()

	payloads := map[string]map[string]interface{}{}
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "?", 2)[0]
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			var payload map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decoding %s payload: %v", endpoint, err)
			}
			payloads[endpoint] = payload
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if endpoint == "application.readTraefikConfig" {
			reads++
			_ = json.NewEncoder(w).Encode(storedConfig)
			return
		}
		t.Errorf("unexpected GET %s", endpoint)
	}))
	t.Cleanup(server.Close)

	return &ApplicationResource{client: client.NewDokployClient(server.URL, "test")}, payloads, &reads
}

const generatedTraefikConfig = "http:\n  routers:\n    app-router-1:\n      rule: Host(`app.example.com`)\n"

// Dokploy writes whatever updateTraefikConfig receives straight to the app's
// router file, so an unset traefik_config must not be "cleared" with "".
func TestSyncTraefikConfigLeavesUnsetConfigAlone(t *testing.T) {
	r, payloads, _ := newTraefikTestResource(t, generatedTraefikConfig)

	if err := r.syncTraefikConfig("app-1", &ApplicationResourceModel{TraefikConfig: types.StringNull()}); err != nil {
		t.Fatalf("syncTraefikConfig: %v", err)
	}

	if payload, ok := payloads["application.updateTraefikConfig"]; ok {
		t.Errorf("updateTraefikConfig was called with %v, want no call", payload)
	}
}

func TestSyncTraefikConfigWritesConfiguredValue(t *testing.T) {
	r, payloads, _ := newTraefikTestResource(t, "")

	custom := "http:\n  middlewares: {}\n"
	if err := r.syncTraefikConfig("app-1", &ApplicationResourceModel{TraefikConfig: types.StringValue(custom)}); err != nil {
		t.Fatalf("syncTraefikConfig: %v", err)
	}

	if got := payloads["application.updateTraefikConfig"]["traefikConfig"]; got != custom {
		t.Errorf("traefikConfig = %q, want %q", got, custom)
	}
}

// The generated router file must not leak into state while traefik_config is
// unmanaged: with the attribute null in config, a stored value is what the
// next update would try to clear.
func TestManagedTraefikConfigStaysNullWhenUnmanaged(t *testing.T) {
	r, _, reads := newTraefikTestResource(t, generatedTraefikConfig)

	got, err := r.managedTraefikConfig("app-1", types.StringNull())
	if err != nil {
		t.Fatalf("managedTraefikConfig: %v", err)
	}

	if !got.IsNull() {
		t.Errorf("traefik_config = %q, want null", got.ValueString())
	}
	if *reads != 0 {
		t.Errorf("readTraefikConfig called %d times, want 0", *reads)
	}
}

func TestManagedTraefikConfigReadsWhenManaged(t *testing.T) {
	r, _, _ := newTraefikTestResource(t, generatedTraefikConfig)

	got, err := r.managedTraefikConfig("app-1", types.StringValue("http: {}\n"))
	if err != nil {
		t.Fatalf("managedTraefikConfig: %v", err)
	}

	if got.ValueString() != generatedTraefikConfig {
		t.Errorf("traefik_config = %q, want the stored file", got.ValueString())
	}
}
