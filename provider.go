// Package natskvprovider is a Traefik provider plugin that reads dynamic
// configuration from a NATS KV bucket.
//
// Keys in the bucket follow the pattern:
//
//	http/routers/<name>      → JSON of dynamic.Router
//	http/services/<name>     → JSON of dynamic.Service
//	http/middlewares/<name>  → JSON of dynamic.Middleware
//	tcp/routers/<name>       → JSON of dynamic.TCPRouter
//	tcp/services/<name>      → JSON of dynamic.TCPService
//	udp/routers/<name>       → JSON of dynamic.UDPRouter
//	udp/services/<name>      → JSON of dynamic.UDPService
//	tls/options/<name>       → JSON of tls.Options
//	tls/stores/<name>        → JSON of tls.Store
package natskvprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/traefik/genconf/dynamic"
	"github.com/traefik/genconf/dynamic/tls"
)

// Config is the plugin configuration.
type Config struct {
	NatsURL      string `json:"natsUrl,omitempty"`
	Bucket       string `json:"bucket,omitempty"`
	PollInterval string `json:"pollInterval,omitempty"`
	NKey         string `json:"nkey,omitempty"`
	CredFile     string `json:"credFile,omitempty"`
	Token        string `json:"token,omitempty"`
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{
		NatsURL:      nats.DefaultURL,
		Bucket:       "traefik",
		PollInterval: "5s",
	}
}

// Provider is a Traefik provider plugin backed by NATS KV.
type Provider struct {
	name         string
	natsURL      string
	bucket       string
	pollInterval time.Duration
	nkey         string
	credFile     string
	token        string
	username     string
	password     string

	cancel func()
	conn   *nats.Conn
}

// New creates a new Provider plugin.
func New(_ context.Context, config *Config, name string) (*Provider, error) {
	pi, err := time.ParseDuration(config.PollInterval)
	if err != nil {
		return nil, fmt.Errorf("invalid poll interval %q: %w", config.PollInterval, err)
	}

	return &Provider{
		name:         name,
		natsURL:      config.NatsURL,
		bucket:       config.Bucket,
		pollInterval: pi,
		nkey:         config.NKey,
		credFile:     config.CredFile,
		token:        config.Token,
		username:     config.Username,
		password:     config.Password,
	}, nil
}

// Init validates the provider configuration.
func (p *Provider) Init() error {
	if p.pollInterval <= 0 {
		return fmt.Errorf("poll interval must be greater than 0")
	}

	if p.natsURL == "" {
		return fmt.Errorf("NATS URL must not be empty")
	}

	if p.bucket == "" {
		return fmt.Errorf("bucket name must not be empty")
	}

	return nil
}

// Provide creates and sends dynamic configuration.
func (p *Provider) Provide(cfgChan chan<- json.Marshaler) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	go func() {
		defer func() {
			if err := recover(); err != nil {
				log.Print(err)
			}
		}()

		p.loadConfiguration(ctx, cfgChan)
	}()

	return nil
}

// Stop stops the provider and cleans up resources.
func (p *Provider) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}

	if p.conn != nil {
		p.conn.Close()
	}

	return nil
}

func (p *Provider) connectNATS() (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Name(fmt.Sprintf("traefik-provider-%s", p.name)),
	}

	if p.credFile != "" {
		opts = append(opts, nats.UserCredentials(p.credFile))
	}

	if p.nkey != "" {
		opt, err := nats.NkeyOptionFromSeed(p.nkey)
		if err != nil {
			return nil, fmt.Errorf("failed to configure NKey: %w", err)
		}
		opts = append(opts, opt)
	}

	if p.token != "" {
		opts = append(opts, nats.Token(p.token))
	}

	if p.username != "" && p.password != "" {
		opts = append(opts, nats.UserInfo(p.username, p.password))
	}

	nc, err := nats.Connect(p.natsURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS at %s: %w", p.natsURL, err)
	}

	return nc, nil
}

func (p *Provider) loadConfiguration(ctx context.Context, cfgChan chan<- json.Marshaler) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cfg, err := p.buildConfiguration()
			if err != nil {
				log.Printf("[%s] error building configuration from NATS KV: %v", p.name, err)
				continue
			}

			cfgChan <- &dynamic.JSONPayload{Configuration: cfg}

		case <-ctx.Done():
			return
		}
	}
}

func (p *Provider) getKeyValueStore() (nats.KeyValue, error) {
	if p.conn == nil || !p.conn.IsConnected() {
		nc, err := p.connectNATS()
		if err != nil {
			return nil, err
		}
		p.conn = nc
	}

	js, err := p.conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	kv, err := js.KeyValue(p.bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to access KV bucket %q: %w", p.bucket, err)
	}

	return kv, nil
}

func (p *Provider) buildConfiguration() (*dynamic.Configuration, error) {
	kv, err := p.getKeyValueStore()
	if err != nil {
		return nil, err
	}

	keys, err := kv.Keys()
	if err != nil {
		// nats.ErrNoKeysFound means bucket is empty — return empty config.
		if err == nats.ErrNoKeysFound {
			return emptyConfiguration(), nil
		}
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	cfg := emptyConfiguration()

	for _, key := range keys {
		entry, err := kv.Get(key)
		if err != nil {
			log.Printf("[%s] error reading key %q: %v", p.name, key, err)
			continue
		}

		if err := applyEntry(cfg, key, entry.Value()); err != nil {
			log.Printf("[%s] error applying key %q: %v", p.name, key, err)
		}
	}

	return cfg, nil
}

func applyEntry(cfg *dynamic.Configuration, key string, data []byte) error {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) != 3 {
		return fmt.Errorf("invalid key format %q: expected <type>/<kind>/<name>", key)
	}

	section, kind, name := parts[0], parts[1], parts[2]

	switch section {
	case "http":
		return applyHTTPEntry(cfg, kind, name, data)
	case "tcp":
		return applyTCPEntry(cfg, kind, name, data)
	case "udp":
		return applyUDPEntry(cfg, kind, name, data)
	case "tls":
		return applyTLSEntry(cfg, kind, name, data)
	default:
		return fmt.Errorf("unknown section %q", section)
	}
}

func applyHTTPEntry(cfg *dynamic.Configuration, kind, name string, data []byte) error {
	switch kind {
	case "routers":
		var v dynamic.Router
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("unmarshal router %q: %w", name, err)
		}
		cfg.HTTP.Routers[name] = &v

	case "services":
		var v dynamic.Service
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("unmarshal service %q: %w", name, err)
		}
		cfg.HTTP.Services[name] = &v

	case "middlewares":
		var v dynamic.Middleware
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("unmarshal middleware %q: %w", name, err)
		}
		cfg.HTTP.Middlewares[name] = &v

	case "serversTransports":
		var v dynamic.ServersTransport
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("unmarshal servers transport %q: %w", name, err)
		}
		cfg.HTTP.ServersTransports[name] = &v

	default:
		return fmt.Errorf("unknown HTTP kind %q", kind)
	}

	return nil
}

func applyTCPEntry(cfg *dynamic.Configuration, kind, name string, data []byte) error {
	switch kind {
	case "routers":
		var v dynamic.TCPRouter
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("unmarshal TCP router %q: %w", name, err)
		}
		cfg.TCP.Routers[name] = &v

	case "services":
		var v dynamic.TCPService
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("unmarshal TCP service %q: %w", name, err)
		}
		cfg.TCP.Services[name] = &v

	default:
		return fmt.Errorf("unknown TCP kind %q", kind)
	}

	return nil
}

func applyUDPEntry(cfg *dynamic.Configuration, kind, name string, data []byte) error {
	switch kind {
	case "routers":
		var v dynamic.UDPRouter
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("unmarshal UDP router %q: %w", name, err)
		}
		cfg.UDP.Routers[name] = &v

	case "services":
		var v dynamic.UDPService
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("unmarshal UDP service %q: %w", name, err)
		}
		cfg.UDP.Services[name] = &v

	default:
		return fmt.Errorf("unknown UDP kind %q", kind)
	}

	return nil
}

func applyTLSEntry(cfg *dynamic.Configuration, kind, name string, data []byte) error {
	switch kind {
	case "options":
		var v tls.Options
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("unmarshal TLS options %q: %w", name, err)
		}
		cfg.TLS.Options[name] = v

	case "stores":
		var v tls.Store
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("unmarshal TLS store %q: %w", name, err)
		}
		cfg.TLS.Stores[name] = v

	case "certificates":
		var v tls.CertAndStores
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("unmarshal TLS certificate %q: %w", name, err)
		}
		cfg.TLS.Certificates = append(cfg.TLS.Certificates, &v)

	default:
		return fmt.Errorf("unknown TLS kind %q", kind)
	}

	return nil
}

func emptyConfiguration() *dynamic.Configuration {
	return &dynamic.Configuration{
		HTTP: &dynamic.HTTPConfiguration{
			Routers:           make(map[string]*dynamic.Router),
			Middlewares:       make(map[string]*dynamic.Middleware),
			Services:          make(map[string]*dynamic.Service),
			ServersTransports: make(map[string]*dynamic.ServersTransport),
		},
		TCP: &dynamic.TCPConfiguration{
			Routers:  make(map[string]*dynamic.TCPRouter),
			Services: make(map[string]*dynamic.TCPService),
		},
		TLS: &dynamic.TLSConfiguration{
			Stores:  make(map[string]tls.Store),
			Options: make(map[string]tls.Options),
		},
		UDP: &dynamic.UDPConfiguration{
			Routers:  make(map[string]*dynamic.UDPRouter),
			Services: make(map[string]*dynamic.UDPService),
		},
	}
}
