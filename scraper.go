package cloudflarereceiver

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

// scraper fetches Cloudflare Analytics GraphQL data and converts it to
// pmetric.Metrics. It mirrors the logic in the Python CloudflareCollector:
//   - two queries per scrape: detailed (dimensions) + total (aggregated)
//   - tags list → zone name map for the cloudflare.zone attribute
type scraper struct {
	cfg        *Config
	client     *gqlClient
	tagToZone  map[string]string // zone tag → zone name
	tags       []string          // list of zone tags passed to GraphQL
	logger     *zap.Logger
}

func newScraper(cfg *Config, logger *zap.Logger) *scraper {
	tags := make([]string, 0, len(cfg.Zones))
	tagToZone := make(map[string]string, len(cfg.Zones))
	for _, z := range cfg.Zones {
		tags = append(tags, z.Tag)
		tagToZone[z.Tag] = z.Name
	}
	return &scraper{
		cfg:       cfg,
		client:    newGQLClient(cfg),
		tagToZone: tagToZone,
		tags:      tags,
		logger:    logger,
	}
}

// scrape executes both GraphQL queries and returns all metrics as pmetric.Metrics.
func (s *scraper) scrape(ctx context.Context) (pmetric.Metrics, error) {
	md := pmetric.NewMetrics()
	vars := buildVars(s.tags, s.cfg.ItemsLimit)

	if err := s.scrapeDetailed(ctx, vars, md); err != nil {
		s.logger.Warn("detailed query failed", zap.Error(err))
	}
	if err := s.scrapeTotal(ctx, vars, md); err != nil {
		s.logger.Warn("total query failed", zap.Error(err))
	}

	return md, nil
}

// ----------------------------------------------------------------------------
// Detailed metrics (with dimensions)
// ----------------------------------------------------------------------------

func (s *scraper) scrapeDetailed(ctx context.Context, vars map[string]any, md pmetric.Metrics) error {
	resp, err := s.client.queryDetailed(ctx, vars)
	if err != nil {
		return fmt.Errorf("queryDetailed: %w", err)
	}

	now := pcommon.NewTimestampFromTime(time.Now())

	for _, zone := range resp.Data.Viewer.Zones {
		zoneName := s.tagToZone[zone.ZoneTag]
		if zoneName == "" {
			zoneName = zone.ZoneTag
		}

		rm := md.ResourceMetrics().AppendEmpty()
		rm.Resource().Attributes().PutStr("cloudflare.zone", zoneName)
		sm := rm.ScopeMetrics().AppendEmpty()
		sm.Scope().SetName("otelcol/cloudflarereceiver")

		s.addFirewallMetrics(sm, zone, zoneName, now)
		s.addHTTPMetrics(sm, zone, zoneName, now)
		s.addLBMetrics(sm, zone, zoneName, now)
		s.addHealthCheckMetrics(sm, zone, zoneName, now)
		s.addLogpushMetrics(sm, zone, zoneName, now)
	}
	return nil
}

func (s *scraper) addFirewallMetrics(sm pmetric.ScopeMetrics, zone zoneDetailed, zoneName string, now pcommon.Timestamp) {
	if len(zone.FirewallEventsAdaptiveGroups) == 0 {
		return
	}
	m := appendGauge(sm, "cloudflare.firewall.events", "Number of Cloudflare firewall events grouped by host and action", "{events}")
	for _, row := range zone.FirewallEventsAdaptiveGroups {
		dp := m.Gauge().DataPoints().AppendEmpty()
		dp.SetTimestamp(now)
		dp.SetIntValue(row.Count)
		dp.Attributes().PutStr("cloudflare.zone", zoneName)
		dp.Attributes().PutStr("action", row.Dimensions.Action)
		dp.Attributes().PutStr("client.request.http.host", row.Dimensions.ClientRequestHTTPHost)
	}
}

func (s *scraper) addHTTPMetrics(sm pmetric.ScopeMetrics, zone zoneDetailed, zoneName string, now pcommon.Timestamp) {
	if len(zone.HTTPRequestsAdaptiveGroups) == 0 {
		return
	}
	m := appendGauge(sm, "cloudflare.http.requests", "Number of Cloudflare HTTP requests grouped by host", "{requests}")
	for _, row := range zone.HTTPRequestsAdaptiveGroups {
		dp := m.Gauge().DataPoints().AppendEmpty()
		dp.SetTimestamp(now)
		dp.SetIntValue(row.Count)
		dp.Attributes().PutStr("cloudflare.zone", zoneName)
		dp.Attributes().PutStr("client.request.http.host", row.Dimensions.ClientRequestHTTPHost)
	}
}

func (s *scraper) addLBMetrics(sm pmetric.ScopeMetrics, zone zoneDetailed, zoneName string, now pcommon.Timestamp) {
	if len(zone.LoadBalancingRequestsAdaptiveGroups) == 0 {
		return
	}
	m := appendGauge(sm, "cloudflare.loadbalancing.requests", "Number of Cloudflare load balancing requests grouped by LB name and proxy status", "{requests}")
	for _, row := range zone.LoadBalancingRequestsAdaptiveGroups {
		dp := m.Gauge().DataPoints().AppendEmpty()
		dp.SetTimestamp(now)
		dp.SetIntValue(row.Count)
		dp.Attributes().PutStr("cloudflare.zone", zoneName)
		dp.Attributes().PutStr("lb.name", row.Dimensions.LBName)
		dp.Attributes().PutBool("proxied", bool(row.Dimensions.Proxied))
	}
}

func (s *scraper) addHealthCheckMetrics(sm pmetric.ScopeMetrics, zone zoneDetailed, zoneName string, now pcommon.Timestamp) {
	if len(zone.HealthCheckEventsAdaptiveGroups) == 0 {
		return
	}
	m := appendGauge(sm, "cloudflare.healthcheck.events", "Number of Cloudflare health check events grouped by target and status", "{events}")
	for _, row := range zone.HealthCheckEventsAdaptiveGroups {
		dp := m.Gauge().DataPoints().AppendEmpty()
		dp.SetTimestamp(now)
		dp.SetIntValue(row.Count)
		dp.Attributes().PutStr("cloudflare.zone", zoneName)
		dp.Attributes().PutStr("fqdn", row.Dimensions.FQDN)
		dp.Attributes().PutStr("health.status", row.Dimensions.HealthStatus)
		dp.Attributes().PutBool("health.changed", bool(row.Dimensions.HealthChanged))
		dp.Attributes().PutStr("health.check.name", row.Dimensions.HealthCheckName)
	}
}

func (s *scraper) addLogpushMetrics(sm pmetric.ScopeMetrics, zone zoneDetailed, zoneName string, now pcommon.Timestamp) {
	if len(zone.LogpushHealthAdaptiveGroups) == 0 {
		return
	}
	m := appendGauge(sm, "cloudflare.logpush.events", "Number of Cloudflare logpush events grouped by status", "{events}")
	for _, row := range zone.LogpushHealthAdaptiveGroups {
		dp := m.Gauge().DataPoints().AppendEmpty()
		dp.SetTimestamp(now)
		dp.SetIntValue(row.Count)
		dp.Attributes().PutStr("cloudflare.zone", zoneName)
		dp.Attributes().PutStr("logpush.status", string(row.Dimensions.Status))
	}
}

// ----------------------------------------------------------------------------
// Total metrics (zone-level aggregates, no extra dimensions)
// ----------------------------------------------------------------------------

func (s *scraper) scrapeTotal(ctx context.Context, vars map[string]any, md pmetric.Metrics) error {
	resp, err := s.client.queryTotal(ctx, vars)
	if err != nil {
		return fmt.Errorf("queryTotal: %w", err)
	}

	now := pcommon.NewTimestampFromTime(time.Now())

	for _, zone := range resp.Data.Viewer.Zones {
		zoneName := s.tagToZone[zone.ZoneTag]
		if zoneName == "" {
			zoneName = zone.ZoneTag
		}

		rm := md.ResourceMetrics().AppendEmpty()
		rm.Resource().Attributes().PutStr("cloudflare.zone", zoneName)
		sm := rm.ScopeMetrics().AppendEmpty()
		sm.Scope().SetName("otelcol/cloudflarereceiver")

		addTotalMetric(sm, "cloudflare.firewall.events.total",
			"Total number of Cloudflare firewall events per zone",
			zone.FirewallEventsAdaptiveGroups, zoneName, now)

		addTotalMetric(sm, "cloudflare.http.requests.total",
			"Total number of Cloudflare HTTP requests per zone",
			zone.HTTPRequestsAdaptiveGroups, zoneName, now)

		addTotalMetric(sm, "cloudflare.loadbalancing.requests.total",
			"Total number of Cloudflare load balancing requests per zone",
			zone.LoadBalancingRequestsAdaptiveGroups, zoneName, now)

		addTotalMetric(sm, "cloudflare.healthcheck.events.total",
			"Total number of Cloudflare health check events per zone",
			zone.HealthCheckEventsAdaptiveGroups, zoneName, now)

		addTotalMetric(sm, "cloudflare.logpush.events.total",
			"Total number of Cloudflare logpush events per zone",
			zone.LogpushHealthAdaptiveGroups, zoneName, now)
	}
	return nil
}

// addTotalMetric sums all count rows into a single data point (matching the
// Python _get_total_metrics behaviour where each row is set individually, but
// each zone only has one total group because the query has no dimensions).
func addTotalMetric(sm pmetric.ScopeMetrics, name, desc string, rows []countOnly, zoneName string, now pcommon.Timestamp) {
	if len(rows) == 0 {
		return
	}
	m := appendGauge(sm, name, desc, "{events}")
	var total int64
	for _, r := range rows {
		total += r.Count
	}
	dp := m.Gauge().DataPoints().AppendEmpty()
	dp.SetTimestamp(now)
	dp.SetIntValue(total)
	dp.Attributes().PutStr("cloudflare.zone", zoneName)
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func appendGauge(sm pmetric.ScopeMetrics, name, desc, unit string) pmetric.Metric {
	m := sm.Metrics().AppendEmpty()
	m.SetName(name)
	m.SetDescription(desc)
	m.SetUnit(unit)
	m.SetEmptyGauge()
	return m
}
