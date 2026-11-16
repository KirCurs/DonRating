package config

import (
	"strings"
	"testing"
	"time"
)

func fromMap(values map[string]string) (Config, error) {
	return load(func(key string) string { return values[key] })
}

func TestDefaultsAllowLoopbackDemo(t *testing.T) {
	cfg, err := fromMap(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:8080" || cfg.RequestTTL != 30*time.Second || len(cfg.Devices) != 1 || cfg.Devices[0].ID != "reception-1" {
		t.Fatalf("unexpected defaults: address=%q ttl=%s devices=%v", cfg.Addr, cfg.RequestTTL, cfg.Devices)
	}
	if cfg.AdminToken != "" || len(cfg.DeviceTokens) != 0 {
		t.Fatal("local demo unexpectedly requires credentials")
	}
}

func TestNetworkBindRequiresEveryCredential(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8080", ":8080", "[::]:8080", "reception.local:8080"} {
		t.Run(addr, func(t *testing.T) {
			values := map[string]string{"APP_ADDR": addr}
			if _, err := fromMap(values); err == nil {
				t.Fatal("network bind accepted without admin key")
			}
			values["ADMIN_TOKEN"] = "admin-credential-0123456789-abcdef"
			if _, err := fromMap(values); err == nil {
				t.Fatal("network bind accepted without tablet key")
			}
			values["DEVICE_TOKENS_JSON"] = `{"reception-1":"tablet-credential-0123456789-abcdef"}`
			if _, err := fromMap(values); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLoopbackBindings(t *testing.T) {
	for _, addr := range []string{"localhost:8080", "127.0.0.1:8080", "[::1]:8080"} {
		if _, err := fromMap(map[string]string{"APP_ADDR": addr}); err != nil {
			t.Errorf("%s: %v", addr, err)
		}
	}
}

func TestInvalidSettings(t *testing.T) {
	cases := map[string]map[string]string{
		"address":               {"APP_ADDR": "8080"},
		"port":                  {"APP_ADDR": "127.0.0.1:70000"},
		"duration":              {"REQUEST_TTL": "tomorrow"},
		"short ttl":             {"REQUEST_TTL": "4s"},
		"long ttl":              {"REQUEST_TTL": "601s"},
		"empty devices":         {"DEVICES_JSON": "[]"},
		"null devices":          {"DEVICES_JSON": "null"},
		"duplicate ids":         {"DEVICES_JSON": `[{"id":"a","name":"One"},{"id":"a","name":"Two"}]`},
		"unsafe id":             {"DEVICES_JSON": `[{"id":"../admin","name":"One"}]`},
		"missing name":          {"DEVICES_JSON": `[{"id":"a"}]`},
		"unknown field":         {"DEVICES_JSON": `[{"id":"a","name":"One","token":"wrong-place"}]`},
		"trailing json":         {"DEVICES_JSON": `[{"id":"a","name":"One"}] []`},
		"unknown tablet":        {"DEVICE_TOKENS_JSON": `{"unknown":"key"}`},
		"null keys":             {"DEVICE_TOKENS_JSON": "null"},
		"empty key":             {"DEVICE_TOKENS_JSON": `{"reception-1":""}`},
		"same admin and tablet": {"ADMIN_TOKEN": "shared", "DEVICE_TOKENS_JSON": `{"reception-1":"shared"}`},
		"whitespace admin":      {"ADMIN_TOKEN": "secret\nkey"},
		"whitespace tablet":     {"DEVICE_TOKENS_JSON": `{"reception-1":"secret key"}`},
		"same tablet keys":      {"DEVICES_JSON": `[{"id":"a","name":"One"},{"id":"b","name":"Two"}]`, "DEVICE_TOKENS_JSON": `{"a":"shared","b":"shared"}`},
	}
	for name, values := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := fromMap(values); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestTTLLimitsAndSeparateTabletKeys(t *testing.T) {
	for _, ttl := range []string{"5s", "10m"} {
		if _, err := fromMap(map[string]string{"REQUEST_TTL": ttl}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := fromMap(map[string]string{
		"APP_ADDR": "0.0.0.0:8080", "ADMIN_TOKEN": strings.Repeat("a", 24),
		"DEVICES_JSON":       `[{"id":"a","name":"One"},{"id":"b","name":"Two"}]`,
		"DEVICE_TOKENS_JSON": `{"a":"aaaaaaaaaaaaaaaaaaaaaaab"}`,
	})
	if err == nil {
		t.Fatal("missing second tablet credential accepted")
	}
}

func TestErrorsDoNotContainCredentials(t *testing.T) {
	secret := "do-not-log-this-secret\n"
	_, err := fromMap(map[string]string{"ADMIN_TOKEN": secret})
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "do-not-log") {
		t.Fatal("credential leaked in configuration error")
	}
}
