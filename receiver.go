package cloudflarereceiver

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.uber.org/zap"
)

// cloudflareReceiver is the OTel receiver component. It runs a polling loop
// that calls the scraper on every CollectionInterval and forwards the resulting
// pmetric.Metrics to the downstream consumer.
type cloudflareReceiver struct {
	cfg      *Config
	consumer consumer.Metrics
	scraper  *scraper
	logger   *zap.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newReceiver(cfg *Config, consumer consumer.Metrics, logger *zap.Logger) *cloudflareReceiver {
	return &cloudflareReceiver{
		cfg:      cfg,
		consumer: consumer,
		scraper:  newScraper(cfg, logger),
		logger:   logger,
	}
}

// Start launches the polling goroutine. Called by the collector runtime.
func (r *cloudflareReceiver) Start(_ context.Context, _ component.Host) error {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	r.wg.Add(1)
	go r.pollLoop(ctx)

	r.logger.Info("Cloudflare receiver started",
		zap.Duration("collection_interval", r.cfg.CollectionInterval),
		zap.Strings("zones", zoneNames(r.cfg.Zones)),
	)
	return nil
}

// Shutdown stops the polling loop and waits for it to finish.
func (r *cloudflareReceiver) Shutdown(_ context.Context) error {
	r.cancel()
	r.wg.Wait()
	r.logger.Info("Cloudflare receiver stopped")
	return nil
}

func (r *cloudflareReceiver) pollLoop(ctx context.Context) {
	defer r.wg.Done()

	// Scrape immediately on start so we don't wait a full interval before
	// the first data point appears (mirrors the Python collector's run-on-start
	// behaviour).
	r.collect(ctx)

	ticker := time.NewTicker(r.cfg.CollectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.collect(ctx)
		}
	}
}

func (r *cloudflareReceiver) collect(ctx context.Context) {
	r.logger.Debug("Starting Cloudflare scrape")
	md, err := r.scraper.scrape(ctx)
	if err != nil {
		r.logger.Error("Scrape failed", zap.Error(err))
		return
	}

	if md.DataPointCount() == 0 {
		r.logger.Debug("Scrape returned no data points")
		return
	}

	if err := r.consumer.ConsumeMetrics(ctx, md); err != nil {
		r.logger.Error("Failed to send metrics to consumer", zap.Error(err))
	}
}

func zoneNames(zones []ZoneConfig) []string {
	names := make([]string, 0, len(zones))
	for _, z := range zones {
		names = append(names, z.Name)
	}
	return names
}
