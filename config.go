package cloudflarereceiver

import (
	"errors"
	"fmt"
	"time"
)

// ZoneConfig maps a human-readable zone name to its Cloudflare zone tag (ID).
type ZoneConfig struct {
	// Name is the zone domain name, used as the cloudflare.zone metric attribute.
	Name string `mapstructure:"name"`
	// Tag is the Cloudflare zone ID used in GraphQL queries.
	Tag string `mapstructure:"tag"`
}

// Config represents the receiver configuration loaded from otelcol YAML.
type Config struct {
	// Endpoint is the Cloudflare GraphQL Analytics API URL.
	// Default: https://api.cloudflare.com/client/v4/graphql
	Endpoint string `mapstructure:"endpoint"`

	// APIKey is the Cloudflare API token sent as the Authorization header.
	// Format: "Bearer <token>"
	APIKey string `mapstructure:"api_key"`

	// Username is the Cloudflare account email sent as X-AUTH-EMAIL.
	Username string `mapstructure:"username"`

	// Zones is the list of Cloudflare zones to collect metrics for.
	//
	// Example:
	//   zones:
	//     - name: example.com
	//       tag: abc123zoneid
	Zones []ZoneConfig `mapstructure:"zones"`

	// ItemsLimit is the maximum number of rows per GraphQL group-by query.
	// Cloudflare allows up to 10000. Default: 10000.
	ItemsLimit int `mapstructure:"items_limit"`

	// Timeout for each GraphQL request. Default: 30s.
	Timeout time.Duration `mapstructure:"timeout"`

	// Retries is how many times to retry a failed GraphQL request before
	// giving up. Default: 3.
	Retries int `mapstructure:"retries"`

	// CollectionInterval determines how often metrics are collected.
	// Default: 3600s (1 hour) — Cloudflare daily buckets don't change rapidly.
	CollectionInterval time.Duration `mapstructure:"collection_interval"`
}

const (
	defaultEndpoint           = "https://api.cloudflare.com/client/v4/graphql"
	defaultItemsLimit         = 10000
	defaultTimeout            = 30 * time.Second
	defaultRetries            = 3
	defaultCollectionInterval = 3600 * time.Second
)

func createDefaultConfig() *Config {
	return &Config{
		Endpoint:           defaultEndpoint,
		ItemsLimit:         defaultItemsLimit,
		Timeout:            defaultTimeout,
		Retries:            defaultRetries,
		CollectionInterval: defaultCollectionInterval,
	}
}

func (c *Config) Validate() error {
	if c.APIKey == "" {
		return errors.New("api_key must be set")
	}
	if c.Username == "" {
		return errors.New("username must be set")
	}
	if len(c.Zones) == 0 {
		return errors.New("at least one zone must be configured")
	}
	for i, z := range c.Zones {
		if z.Name == "" {
			return fmt.Errorf("zones[%d]: name must be set", i)
		}
		if z.Tag == "" {
			return fmt.Errorf("zones[%d]: tag must be set", i)
		}
	}
	if c.ItemsLimit <= 0 || c.ItemsLimit > 10000 {
		return errors.New("items_limit must be between 1 and 10000")
	}
	if c.CollectionInterval <= 0 {
		return errors.New("collection_interval must be positive")
	}
	return nil
}
