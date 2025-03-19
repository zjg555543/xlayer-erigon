package stages

import (
	"context"
	"fmt"

	"github.com/ledgerwatch/erigon/core/rawdb"
	"github.com/ledgerwatch/erigon/eth/stagedsync"
	"github.com/ledgerwatch/log/v3"
)

func alignExecutionToSMT(batchContext *BatchContext, lastExecutedBlock, smtMaxBlockNumber uint64, u stagedsync.Unwinder) (bool, error) {
	if lastExecutedBlock > smtMaxBlockNumber {
		block, err := rawdb.ReadBlockByNumber(batchContext.sdb.tx, smtMaxBlockNumber)
		if err != nil {
			return false, err
		}

		log.Warn(fmt.Sprintf("[%s] Unwinding due to SMT gap", batchContext.s.LogPrefix()), "smtHeight", smtMaxBlockNumber, "sequencerHeight", lastExecutedBlock)
		u.UnwindTo(smtMaxBlockNumber, stagedsync.BadBlock(block.Hash(), fmt.Errorf("received bad block")))
		return true, nil
	}
	return false, nil
}

func resequenceFromSMTAlignment(
	s *stagedsync.StageState,
	u stagedsync.Unwinder,
	ctx context.Context,
	cfg SequenceBlockCfg,
	historyCfg stagedsync.HistoryCfg,
	lastBatch, highestBatchInDs uint64,
) (err error) {
	log.Info(fmt.Sprintf("[%s] ResequenceFromSMTAlignment, last batch %d is lower than highest batch in datastream %d, resequencing...", s.LogPrefix(), lastBatch, highestBatchInDs))

	batches, err := cfg.dataStreamServer.ReadBatches(lastBatch+1, highestBatchInDs)
	if err != nil {
		return err
	}

	if err = cfg.dataStreamServer.UnwindToBatchStart(lastBatch + 1); err != nil {
		return err
	}

	log.Info(fmt.Sprintf("[%s] ResequenceFromSMTAlignment, from batch %d to %d in data stream", s.LogPrefix(), lastBatch+1, highestBatchInDs))
	for _, batch := range batches {
		batchJob := NewResequenceBatchJob(batch)
		subBatchCount := 0
		for batchJob.HasMoreBlockToProcess() {
			if err = sequencingBatchStep(s, u, ctx, cfg, historyCfg, batchJob); err != nil {
				return err
			}

			subBatchCount += 1
		}

		log.Info(fmt.Sprintf("[%s] ResequenceFromSMTAlignment, original batch %d with %d batches", s.LogPrefix(), batchJob.batchToProcess[0].BatchNumber, subBatchCount))
		if cfg.zk.SequencerResequenceStrict && subBatchCount != 1 {
			return fmt.Errorf("strict mode enabled, but resequenced batch %d has %d sub-batches", batchJob.batchToProcess[0].BatchNumber, subBatchCount)
		}
	}

	return nil
}
