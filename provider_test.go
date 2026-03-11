package natskvprovider

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCreateConfig(t *testing.T) {
	config := CreateConfig()

	if config.NatsURL != "nats://127.0.0.1:4222" {
		t.Fatalf("expected default NATS URL, got %q", config.NatsURL)
	}

	if config.Bucket != "traefik" {
		t.Fatalf("expected default bucket %q, got %q", "traefik", config.Bucket)
	}

	if config.PollInterval != "5s" {
		t.Fatalf("expected default poll interval %q, got %q", "5s", config.PollInterval)
	}
}

func TestNew(t *testing.T) {
	config := CreateConfig()
	config.PollInterval = "1s"

	provider, err := New(context.Background(), config, "test")
	if err != nil {
		t.Fatal(err)
	}

	if provider.name != "test" {
		t.Fatalf("expected name %q, got %q", "test", provider.name)
	}

	if provider.pollInterval.Seconds() != 1 {
		t.Fatalf("expected poll interval 1s, got %v", provider.pollInterval)
	}
}

func TestNew_InvalidPollInterval(t *testing.T) {
	config := CreateConfig()
	config.PollInterval = "invalid"

	_, err := New(context.Background(), config, "test")
	if err == nil {
		t.Fatal("expected error for invalid poll interval")
	}
}

func TestInit_ValidConfig(t *testing.T) {
	config := CreateConfig()
	provider, err := New(context.Background(), config, "test")
	if err != nil {
		t.Fatal(err)
	}

	if err := provider.Init(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInit_ZeroPollInterval(t *testing.T) {
	provider := &Provider{
		natsURL: "nats://localhost:4222",
		bucket:  "traefik",
	}

	if err := provider.Init(); err == nil {
		t.Fatal("expected error for zero poll interval")
	}
}

func TestInit_EmptyURL(t *testing.T) {
	provider := &Provider{
		pollInterval: 1,
		bucket:       "traefik",
	}

	if err := provider.Init(); err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestInit_EmptyBucket(t *testing.T) {
	provider := &Provider{
		pollInterval: 1,
		natsURL:      "nats://localhost:4222",
	}

	if err := provider.Init(); err == nil {
		t.Fatal("expected error for empty bucket")
	}
}

func TestEmptyConfiguration(t *testing.T) {
	cfg := emptyConfiguration()

	if cfg.HTTP == nil {
		t.Fatal("HTTP config should not be nil")
	}
	if cfg.TCP == nil {
		t.Fatal("TCP config should not be nil")
	}
	if cfg.UDP == nil {
		t.Fatal("UDP config should not be nil")
	}
	if cfg.TLS == nil {
		t.Fatal("TLS config should not be nil")
	}
}

func TestApplyEntry_HTTPRouter(t *testing.T) {
	cfg := emptyConfiguration()

	data := []byte(`{"entryPoints":["web"],"service":"my-service","rule":"Host(` + "`example.com`" + `)"}`)

	if err := applyEntry(cfg, "http/routers/my-router", data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	router, ok := cfg.HTTP.Routers["my-router"]
	if !ok {
		t.Fatal("router not found")
	}

	if router.Rule != "Host(`example.com`)" {
		t.Fatalf("unexpected rule: %s", router.Rule)
	}

	if router.Service != "my-service" {
		t.Fatalf("unexpected service: %s", router.Service)
	}
}

func TestApplyEntry_HTTPService(t *testing.T) {
	cfg := emptyConfiguration()

	data := []byte(`{"loadBalancer":{"servers":[{"url":"http://localhost:8080"}],"passHostHeader":true}}`)

	if err := applyEntry(cfg, "http/services/my-service", data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	svc, ok := cfg.HTTP.Services["my-service"]
	if !ok {
		t.Fatal("service not found")
	}

	if svc.LoadBalancer == nil {
		t.Fatal("load balancer should not be nil")
	}

	if len(svc.LoadBalancer.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(svc.LoadBalancer.Servers))
	}

	if svc.LoadBalancer.Servers[0].URL != "http://localhost:8080" {
		t.Fatalf("unexpected URL: %s", svc.LoadBalancer.Servers[0].URL)
	}
}

func TestApplyEntry_HTTPMiddleware(t *testing.T) {
	cfg := emptyConfiguration()

	data := []byte(`{"stripPrefix":{"prefixes":["/api"]}}`)

	if err := applyEntry(cfg, "http/middlewares/my-middleware", data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mw, ok := cfg.HTTP.Middlewares["my-middleware"]
	if !ok {
		t.Fatal("middleware not found")
	}

	if mw.StripPrefix == nil {
		t.Fatal("stripPrefix should not be nil")
	}
}

func TestApplyEntry_TCPRouter(t *testing.T) {
	cfg := emptyConfiguration()

	data := []byte(`{"entryPoints":["tcp"],"service":"my-tcp-service","rule":"HostSNI(` + "`example.com`" + `)"}`)

	if err := applyEntry(cfg, "tcp/routers/my-tcp-router", data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	router, ok := cfg.TCP.Routers["my-tcp-router"]
	if !ok {
		t.Fatal("TCP router not found")
	}

	if router.Service != "my-tcp-service" {
		t.Fatalf("unexpected service: %s", router.Service)
	}
}

func TestApplyEntry_UDPRouter(t *testing.T) {
	cfg := emptyConfiguration()

	data := []byte(`{"entryPoints":["udp"],"service":"my-udp-service"}`)

	if err := applyEntry(cfg, "udp/routers/my-udp-router", data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	router, ok := cfg.UDP.Routers["my-udp-router"]
	if !ok {
		t.Fatal("UDP router not found")
	}

	if router.Service != "my-udp-service" {
		t.Fatalf("unexpected service: %s", router.Service)
	}
}

func TestApplyEntry_TLSOptions(t *testing.T) {
	cfg := emptyConfiguration()

	data := []byte(`{"minVersion":"VersionTLS12"}`)

	if err := applyEntry(cfg, "tls/options/my-tls", data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	opts, ok := cfg.TLS.Options["my-tls"]
	if !ok {
		t.Fatal("TLS options not found")
	}

	if opts.MinVersion != "VersionTLS12" {
		t.Fatalf("unexpected min version: %s", opts.MinVersion)
	}
}

func TestApplyEntry_InvalidKeyFormat(t *testing.T) {
	cfg := emptyConfiguration()

	if err := applyEntry(cfg, "invalid-key", []byte("{}")); err == nil {
		t.Fatal("expected error for invalid key format")
	}
}

func TestApplyEntry_UnknownSection(t *testing.T) {
	cfg := emptyConfiguration()

	if err := applyEntry(cfg, "unknown/routers/test", []byte("{}")); err == nil {
		t.Fatal("expected error for unknown section")
	}
}

func TestApplyEntry_InvalidJSON(t *testing.T) {
	cfg := emptyConfiguration()

	if err := applyEntry(cfg, "http/routers/test", []byte("not-json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestApplyEntry_UnknownHTTPKind(t *testing.T) {
	cfg := emptyConfiguration()

	if err := applyEntry(cfg, "http/unknown/test", []byte("{}")); err == nil {
		t.Fatal("expected error for unknown HTTP kind")
	}
}

func TestApplyEntry_UnknownTCPKind(t *testing.T) {
	cfg := emptyConfiguration()

	if err := applyEntry(cfg, "tcp/unknown/test", []byte("{}")); err == nil {
		t.Fatal("expected error for unknown TCP kind")
	}
}

func TestApplyEntry_UnknownUDPKind(t *testing.T) {
	cfg := emptyConfiguration()

	if err := applyEntry(cfg, "udp/unknown/test", []byte("{}")); err == nil {
		t.Fatal("expected error for unknown UDP kind")
	}
}

func TestApplyEntry_UnknownTLSKind(t *testing.T) {
	cfg := emptyConfiguration()

	if err := applyEntry(cfg, "tls/unknown/test", []byte("{}")); err == nil {
		t.Fatal("expected error for unknown TLS kind")
	}
}

func TestConfigJSON(t *testing.T) {
	config := CreateConfig()

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decoded.Bucket != config.Bucket {
		t.Fatalf("expected bucket %q, got %q", config.Bucket, decoded.Bucket)
	}
}
