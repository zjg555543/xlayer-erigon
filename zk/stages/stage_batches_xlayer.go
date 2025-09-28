package stages

import (
	"context"
	"fmt"

	"github.com/ledgerwatch/erigon/zk/datastream/client"
	"github.com/ledgerwatch/log/v3"
)

// queryClientManager manages a reusable datastream client for high-frequency queries
// This is an X Layer optimization to reduce TCP connection overhead
type queryClientManager struct {
	client     *client.StreamClient
	ctx        context.Context
	cfg        BatchesCfg
	latestFork uint16
}

// newQueryClientManager creates a new query client manager
func newQueryClientManager(ctx context.Context, cfg BatchesCfg, latestFork uint16) *queryClientManager {
	return &queryClientManager{
		ctx:        ctx,
		cfg:        cfg,
		latestFork: latestFork,
	}
}

// getOrCreateClient returns a healthy client, using HandleStart pattern for connection management
func (qcm *queryClientManager) getOrCreateClient() (*client.StreamClient, error) {
	// Create client if not exists
	if qcm.client == nil {
		log.Info("Creating new query client for L2Block queries")
		qcm.client = buildNewStreamClient(qcm.ctx, qcm.cfg, qcm.latestFork)
	}

	// Use HandleStart for intelligent connection management
	// This handles both initial startup and error recovery automatically
	if err := qcm.client.HandleStart(); err != nil {
		// Don't immediately discard client - let HandleStart retry on next call
		// Network errors are handled by HandleStart internally, just return error
		log.Info("Query client HandleStart failed, will retry", "error", err)
		return nil, fmt.Errorf("failed to start/reconnect query client: %w", err)
	}

	return qcm.client, nil
}

// Global query client manager for high-frequency L2Block queries
var globalQueryManager *queryClientManager

// getOrCreateQueryClient is the X Layer optimized client getter
// It maintains a single reusable connection to reduce TCP overhead
func getOrCreateQueryClient(ctx context.Context, cfg BatchesCfg, latestFork uint16) (*client.StreamClient, error) {
	// Initialize manager on first call
	if globalQueryManager == nil {
		globalQueryManager = newQueryClientManager(ctx, cfg, latestFork)
	}

	return globalQueryManager.getOrCreateClient()
}
