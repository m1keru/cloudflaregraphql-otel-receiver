// Package cloudflarereceiver implements an OpenTelemetry Collector receiver
// that polls the Cloudflare Analytics GraphQL API and exposes the results as
// OTLP metrics.
//
// Metrics produced:
//   - cloudflare.firewall.events        (action, client.request.http.host, zone)
//   - cloudflare.firewall.events.total  (zone)
//   - cloudflare.http.requests          (client.request.http.host, zone)
//   - cloudflare.http.requests.total    (zone)
//   - cloudflare.loadbalancing.requests (lb.name, proxied, zone)
//   - cloudflare.loadbalancing.requests.total (zone)
//   - cloudflare.healthcheck.events     (fqdn, health.status, health.changed, health.check.name, zone)
//   - cloudflare.healthcheck.events.total (zone)
//   - cloudflare.logpush.events         (logpush.status, zone)
//   - cloudflare.logpush.events.total   (zone)
//
// The receiver executes two GraphQL queries per collection cycle:
//   - a detailed query returning per-dimension breakdowns
//   - a total query returning zone-level aggregates
//
// Both queries cover the previous calendar day (midnight-to-midnight UTC),
// which is consistent with how Cloudflare's adaptive groups aggregate data.
package cloudflarereceiver
