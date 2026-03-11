# nats-kv-traefik-provider

A [Traefik](https://traefik.io) provider plugin that reads dynamic configuration from a [NATS](https://nats.io) KV (Key-Value) bucket.

## Configuration

### Static Configuration

```yaml
experimental:
  plugins:
    nats-kv:
      moduleName: github.com/tyevco/nats-kv-traefik-provider
      version: v0.1.0

providers:
  plugin:
    nats-kv:
      natsUrl: nats://localhost:4222
      bucket: traefik
      pollInterval: 5s
```

### Plugin Options

| Option         | Description                        | Default                    |
|----------------|------------------------------------|----------------------------|
| `natsUrl`      | NATS server URL                    | `nats://127.0.0.1:4222`   |
| `bucket`       | NATS KV bucket name               | `traefik`                  |
| `pollInterval` | How often to poll for changes      | `5s`                       |
| `credFile`     | Path to NATS credentials file     |                            |
| `nkey`         | NKey seed for authentication       |                            |
| `token`        | Token for authentication           |                            |
| `username`     | Username for authentication        |                            |
| `password`     | Password for authentication        |                            |

## KV Key Structure

Keys in the NATS KV bucket follow the pattern `<section>/<kind>/<name>`, where the value is the JSON-encoded Traefik configuration object.

### Supported Keys

| Key Pattern                          | Value Type             |
|--------------------------------------|------------------------|
| `http/routers/<name>`                | `dynamic.Router`       |
| `http/services/<name>`              | `dynamic.Service`      |
| `http/middlewares/<name>`           | `dynamic.Middleware`   |
| `http/serversTransports/<name>`    | `dynamic.ServersTransport` |
| `tcp/routers/<name>`                | `dynamic.TCPRouter`    |
| `tcp/services/<name>`              | `dynamic.TCPService`   |
| `udp/routers/<name>`                | `dynamic.UDPRouter`    |
| `udp/services/<name>`              | `dynamic.UDPService`   |
| `tls/options/<name>`                | `tls.Options`          |
| `tls/stores/<name>`                | `tls.Store`            |
| `tls/certificates/<name>`          | `tls.CertAndStores`   |

### Example: Adding a Route via NATS CLI

```bash
# Create the KV bucket
nats kv add traefik

# Add a router
nats kv put traefik 'http/routers/my-router' \
  '{"entryPoints":["web"],"service":"my-service","rule":"Host(`example.com`)"}'

# Add a service
nats kv put traefik 'http/services/my-service' \
  '{"loadBalancer":{"servers":[{"url":"http://localhost:8080"}],"passHostHeader":true}}'
```

## Local Development

To test locally with Traefik, place the plugin in a `./plugins-local` directory:

```
./plugins-local/
  └── src/
      └── github.com/
          └── tyevco/
              └── nats-kv-traefik-provider/
                  ├── provider.go
                  ├── go.mod
                  ├── .traefik.yml
                  └── vendor/
```

Then use local plugin mode in your Traefik static configuration:

```yaml
experimental:
  localPlugins:
    nats-kv:
      moduleName: github.com/tyevco/nats-kv-traefik-provider
```
