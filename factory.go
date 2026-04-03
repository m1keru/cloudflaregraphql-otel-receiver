package cloudflarereceiver

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
)

const typeStr = "cloudflarereceiver"

// NewFactory creates the component.Factory for the Cloudflare Analytics receiver.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		component.MustNewType(typeStr),
		func() component.Config { return createDefaultConfig() },
		receiver.WithMetrics(createMetricsReceiver, component.StabilityLevelDevelopment),
	)
}

func createMetricsReceiver(
	_ context.Context,
	settings receiver.CreateSettings,
	cfg component.Config,
	consumer consumer.Metrics,
) (receiver.Metrics, error) {
	rCfg := cfg.(*Config)
	if err := rCfg.Validate(); err != nil {
		return nil, err
	}
	return newReceiver(rCfg, consumer, settings.Logger), nil
}
