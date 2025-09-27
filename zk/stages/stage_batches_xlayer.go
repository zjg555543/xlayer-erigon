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
	lastError  error
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

// getOrCreateClient returns a healthy client, creating new one if needed
func (qcm *queryClientManager) getOrCreateClient() (*client.StreamClient, error) {
	// If we have an error or no client, recreate
	if qcm.client == nil || qcm.lastError != nil {
		if qcm.client != nil {
			// Clean up old connection
			if err := qcm.client.Stop(); err != nil {
				log.Info("Failed to stop old query client", "error", err)
			}
		}

		// Create and start new client
		log.Info("Creating new query client for L2Block queries")
		qcm.client = buildNewStreamClient(qcm.ctx, qcm.cfg, qcm.latestFork)
		if err := qcm.client.Start(); err != nil {
			qcm.lastError = err
			qcm.client = nil
			return nil, fmt.Errorf("failed to start query client: %w", err)
		}
		qcm.lastError = nil
		log.Info("New query client created and started successfully")
	}

	return qcm.client, nil
}

// markError marks the current client as failed for next recreation
func (qcm *queryClientManager) markError(err error) {
	qcm.lastError = err
	log.Info("Query client marked as failed", "error", err)
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

// markQueryClientError marks the global query client as failed
func markQueryClientError(err error) {
	if globalQueryManager != nil {
		globalQueryManager.markError(err)
	}
}
