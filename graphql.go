package cloudflarereceiver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ----------------------------------------------------------------------------
// GraphQL query strings — ported directly from the Python collector
// ----------------------------------------------------------------------------

// queryDetailed fetches per-dimension rows for each adaptive group.
const queryDetailed = `
query (
  $tags: [String!]!
  $filter: ZoneFilter_InputObject!
  $limit: Int!
) {
  viewer {
    zones(filter: { zoneTag_in: $tags }) {
      zoneTag
      loadBalancingRequestsAdaptiveGroups(limit: $limit, filter: $filter) {
        dimensions { lbName proxied }
        count
      }
      httpRequestsAdaptiveGroups(limit: $limit, filter: $filter) {
        dimensions { clientRequestHTTPHost }
        count
      }
      firewallEventsAdaptiveGroups(limit: $limit, filter: $filter) {
        dimensions { clientRequestHTTPHost action }
        count
      }
      healthCheckEventsAdaptiveGroups(limit: $limit, filter: $filter) {
        dimensions { fqdn healthStatus healthChanged healthCheckName }
        count
      }
      logpushHealthAdaptiveGroups(limit: $limit, filter: $filter) {
        dimensions { status }
        count
      }
    }
  }
}`

// queryTotal fetches aggregated totals (no dimensions) per zone.
const queryTotal = `
query (
  $tags: [String!]!
  $filter: ZoneFilter_InputObject!
  $limit: Int!
) {
  viewer {
    zones(filter: { zoneTag_in: $tags }) {
      zoneTag
      loadBalancingRequestsAdaptiveGroups(limit: $limit, filter: $filter) { count }
      httpRequestsAdaptiveGroups(limit: $limit, filter: $filter) { count }
      firewallEventsAdaptiveGroups(limit: $limit, filter: $filter) { count }
      healthCheckEventsAdaptiveGroups(limit: $limit, filter: $filter) { count }
      logpushHealthAdaptiveGroups(limit: $limit, filter: $filter) { count }
    }
  }
}`

// ----------------------------------------------------------------------------
// Response types
// ----------------------------------------------------------------------------

// jsonString unmarshals both JSON strings and numbers as string.
// Cloudflare GraphQL returns some string fields as bare numbers.
type jsonString string

func (s *jsonString) UnmarshalJSON(data []byte) error {
	// Strip quotes if it's a JSON string, otherwise use the raw number.
	if len(data) >= 2 && data[0] == '"' {
		*s = jsonString(data[1 : len(data)-1])
	} else {
		*s = jsonString(data)
	}
	return nil
}

// jsonBool unmarshals both JSON booleans and 0/1 integers as bool.
// Cloudflare GraphQL returns some boolean fields as numeric 0/1.
type jsonBool bool

func (b *jsonBool) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "true", "1":
		*b = true
	case "false", "0":
		*b = false
	default:
		return fmt.Errorf("cannot unmarshal %s into bool", data)
	}
	return nil
}

// detailedResponse is the decoded JSON from queryDetailed.
type detailedResponse struct {
	Data struct {
		Viewer struct {
			Zones []zoneDetailed `json:"zones"`
		} `json:"viewer"`
	} `json:"data"`
	Errors []gqlError `json:"errors"`
}

type zoneDetailed struct {
	ZoneTag                              string                  `json:"zoneTag"`
	LoadBalancingRequestsAdaptiveGroups  []lbRow                 `json:"loadBalancingRequestsAdaptiveGroups"`
	HTTPRequestsAdaptiveGroups           []httpRow               `json:"httpRequestsAdaptiveGroups"`
	FirewallEventsAdaptiveGroups         []firewallRow           `json:"firewallEventsAdaptiveGroups"`
	HealthCheckEventsAdaptiveGroups      []healthCheckRow        `json:"healthCheckEventsAdaptiveGroups"`
	LogpushHealthAdaptiveGroups          []logpushRow            `json:"logpushHealthAdaptiveGroups"`
}

type lbRow struct {
	Dimensions struct {
		LBName  string   `json:"lbName"`
		Proxied jsonBool `json:"proxied"`
	} `json:"dimensions"`
	Count int64 `json:"count"`
}

type httpRow struct {
	Dimensions struct {
		ClientRequestHTTPHost string `json:"clientRequestHTTPHost"`
	} `json:"dimensions"`
	Count int64 `json:"count"`
}

type firewallRow struct {
	Dimensions struct {
		ClientRequestHTTPHost string `json:"clientRequestHTTPHost"`
		Action                string `json:"action"`
	} `json:"dimensions"`
	Count int64 `json:"count"`
}

type healthCheckRow struct {
	Dimensions struct {
		FQDN            string `json:"fqdn"`
		HealthStatus    string `json:"healthStatus"`
		HealthChanged   jsonBool `json:"healthChanged"`
		HealthCheckName string `json:"healthCheckName"`
	} `json:"dimensions"`
	Count int64 `json:"count"`
}

type logpushRow struct {
	Dimensions struct {
		Status jsonString `json:"status"`
	} `json:"dimensions"`
	Count int64 `json:"count"`
}

// totalResponse is the decoded JSON from queryTotal.
type totalResponse struct {
	Data struct {
		Viewer struct {
			Zones []zoneTotal `json:"zones"`
		} `json:"viewer"`
	} `json:"data"`
	Errors []gqlError `json:"errors"`
}

type zoneTotal struct {
	ZoneTag                              string        `json:"zoneTag"`
	LoadBalancingRequestsAdaptiveGroups  []countOnly   `json:"loadBalancingRequestsAdaptiveGroups"`
	HTTPRequestsAdaptiveGroups           []countOnly   `json:"httpRequestsAdaptiveGroups"`
	FirewallEventsAdaptiveGroups         []countOnly   `json:"firewallEventsAdaptiveGroups"`
	HealthCheckEventsAdaptiveGroups      []countOnly   `json:"healthCheckEventsAdaptiveGroups"`
	LogpushHealthAdaptiveGroups          []countOnly   `json:"logpushHealthAdaptiveGroups"`
}

type countOnly struct {
	Count int64 `json:"count"`
}

type gqlError struct {
	Message string `json:"message"`
}

// ----------------------------------------------------------------------------
// Client
// ----------------------------------------------------------------------------

type gqlClient struct {
	endpoint string
	apiKey   string
	username string
	http     *http.Client
	retries  int
}

func newGQLClient(cfg *Config) *gqlClient {
	return &gqlClient{
		endpoint: cfg.Endpoint,
		apiKey:   cfg.APIKey,
		username: cfg.Username,
		http:     &http.Client{Timeout: cfg.Timeout},
		retries:  cfg.Retries,
	}
}

func (c *gqlClient) queryDetailed(ctx context.Context, vars map[string]any) (*detailedResponse, error) {
	var result detailedResponse
	if err := c.execute(ctx, queryDetailed, vars, &result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("cloudflare GraphQL errors: %v", result.Errors[0].Message)
	}
	return &result, nil
}

func (c *gqlClient) queryTotal(ctx context.Context, vars map[string]any) (*totalResponse, error) {
	var result totalResponse
	if err := c.execute(ctx, queryTotal, vars, &result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("cloudflare GraphQL errors: %v", result.Errors[0].Message)
	}
	return &result, nil
}

func (c *gqlClient) execute(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": vars,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for i := 0; i < c.retries; i++ {
		lastErr = c.doRequest(ctx, body, out)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func (c *gqlClient) doRequest(ctx context.Context, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)
	req.Header.Set("X-AUTH-EMAIL", c.username)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, raw)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// ----------------------------------------------------------------------------
// Query variable helpers
// ----------------------------------------------------------------------------

// buildVars builds the GraphQL variable map covering the previous full day
// (midnight-to-midnight UTC), matching the Python collector's window logic.
func buildVars(tags []string, limit int) map[string]any {
	now := time.Now().UTC()
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := end.Add(-24 * time.Hour)

	return map[string]any{
		"tags": tags,
		"filter": map[string]any{
			"datetime_gt": start.Format(time.RFC3339),
			"datetime_lt": end.Format(time.RFC3339),
		},
		"limit": limit,
	}
}
