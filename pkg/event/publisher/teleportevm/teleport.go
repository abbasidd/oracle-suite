//  Copyright (C) 2020 Maker Ecosystem Growth Holdings, INC.
//
//  This program is free software: you can redistribute it and/or modify
//  it under the terms of the GNU Affero General Public License as
//  published by the Free Software Foundation, either version 3 of the
//  License, or (at your option) any later version.
//
//  This program is distributed in the hope that it will be useful,
//  but WITHOUT ANY WARRANTY; without even the implied warranty of
//  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//  GNU Affero General Public License for more details.
//
//  You should have received a copy of the GNU Affero General Public License
//  along with this program.  If not, see <http://www.gnu.org/licenses/>.

package teleportevm

import (
	"context"
	"errors"
	"time"

	"github.com/chronicleprotocol/oracle-suite/pkg/ethereumv2"
	"github.com/chronicleprotocol/oracle-suite/pkg/ethereumv2/types"

	"github.com/chronicleprotocol/oracle-suite/pkg/log"
	"github.com/chronicleprotocol/oracle-suite/pkg/log/null"
	"github.com/chronicleprotocol/oracle-suite/pkg/transport/messages"
	"github.com/chronicleprotocol/oracle-suite/pkg/util/retry"
)

const TeleportEventType = "teleport_evm"
const LoggerTag = "ETHEREUM_TELEPORT"

// retryInterval is the interval between retry attempts in case of an error
// while communicating with a node.
const retryInterval = 5 * time.Second

// teleportTopic0 is Keccak256("TeleportInitialized((bytes32,bytes32,bytes32,bytes32,uint128,uint80,uint48))")
var teleportTopic0 = types.HexToHash("0x61aedca97129bac4264ec6356bd1f66431e65ab80e2d07b7983647d72776f545")

// Config contains a configuration options for EventProvider.
type Config struct {
	// Client is an instance of Ethereum RPC client.
	Client ethereumv2.Client
	// Addresses is a list of contracts from which logs will be fetched.
	Addresses []types.Address
	// Interval specifies how often provider should check for new logs.
	Interval time.Duration
	// PrefetchPeriod specifies how far back in time provider should prefetch
	// logs. It is used only during the initial start of the provider.
	PrefetchPeriod time.Duration
	// BlockLimit specifies how from many blocks logs can be fetched at once.
	BlockLimit uint64
	// BlockConfirmations specifies how many blocks should be confirmed before
	// fetching logs.
	BlockConfirmations uint64
	// Logger is a current logger interface used by the EventProvider.
	Logger log.Logger
}

// EventProvider listens to TeleportGUID events on Ethereum compatible
// blockchains.
//
// https://github.com/makerdao/dss-teleport
//
// It periodically fetches new TeleportGUID events from the blockchain,
// converts them into messages.Event and sends them to the channel provided
// by Events method.
//
// During the initial start of the provider it also fetches older blocks
// until it reaches the block that is older than the prefetch period. This is
// done to fetch events that were emitted before the provider was started.
//
// In the event of an error in communication with a node, whether related to
// network errors or the node itself, the provider will try to repeat requests
// to the node indefinitely.
type EventProvider struct {
	eventCh chan *messages.Event

	// Configuration parameters copied from Config:
	client         ethereumv2.Client
	addresses      []types.Address
	interval       time.Duration
	prefetchPeriod time.Duration
	blockLimit     uint64
	blockConfirms  uint64
	log            log.Logger

	// Used in tests only:
	disablePrefetchEventsRoutine bool
	disableFetchEventsRoutine    bool
}

// New returns a new instance of the EventProvider struct.
func New(cfg Config) (*EventProvider, error) {
	if cfg.Interval == 0 {
		return nil, errors.New("interval is not set")
	}
	if len(cfg.Addresses) == 0 {
		return nil, errors.New("no addresses provided")
	}
	if cfg.BlockLimit <= 0 {
		return nil, errors.New("block limit must be greater than 0")
	}
	if cfg.Logger == nil {
		cfg.Logger = null.New()
	}
	return &EventProvider{
		eventCh:        make(chan *messages.Event),
		client:         cfg.Client,
		interval:       cfg.Interval,
		addresses:      cfg.Addresses,
		prefetchPeriod: cfg.PrefetchPeriod,
		blockLimit:     cfg.BlockLimit,
		blockConfirms:  cfg.BlockConfirmations,
		log:            cfg.Logger.WithField("tag", LoggerTag),
	}, nil
}

// Events implements the publisher.EventPublisher interface.
func (ep *EventProvider) Events() chan *messages.Event {
	return ep.eventCh
}

// Start implements the publisher.EventPublisher interface.
func (ep *EventProvider) Start(ctx context.Context) error {
	if !ep.disablePrefetchEventsRoutine {
		go ep.prefetchEventsRoutine(ctx)
	}
	if !ep.disableFetchEventsRoutine {
		go ep.fetchEventsRoutine(ctx)
	}
	return nil
}

// prefetchEventsRoutine fetches events from older blocks until it reaches the
// block that is older than the prefetch period. This is done to fetch events
// that were emitted before the provider was started.
func (ep *EventProvider) prefetchEventsRoutine(ctx context.Context) {
	if ep.prefetchPeriod == 0 {
		return
	}
	latestBlock, ok := ep.getBlockNumber(ctx)
	if !ok {
		return // Context was canceled.
	}
	for d := ep.blockConfirms; ctx.Err() == nil; d += ep.blockLimit {
		var from, to uint64
		// Because all integer are unsigned, we need to check against
		// underflow.
		if d+ep.blockLimit-1 > latestBlock {
			from = 0
		} else {
			from = latestBlock - (d + ep.blockLimit - 1)
		}
		to = latestBlock - d
		ep.handleEvents(ctx, from, to)
		ts, ok := ep.getBlockTimestamp(ctx, to)
		if !ok {
			return // Context was canceled.
		}
		if from == 0 || time.Since(ts) > ep.prefetchPeriod {
			return // End of the prefetch period reached.
		}
	}
}

// fetchEventsRoutine periodically fetches new TeleportGUID logs from the
// blockchain.
func (ep *EventProvider) fetchEventsRoutine(ctx context.Context) {
	latestBlock, ok := ep.getBlockNumber(ctx)
	if !ok {
		return // Context was canceled.
	}
	t := time.NewTicker(ep.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			currentBlock, ok := ep.getBlockNumber(ctx)
			if !ok {
				return // Context was canceled.
			}
			if currentBlock <= latestBlock {
				continue // There is no new blocks.
			}
			for _, b := range splitBlockRanges(latestBlock+1, currentBlock, ep.blockLimit) {
				from := b[0] - ep.blockConfirms
				to := b[1] - ep.blockConfirms
				ep.handleEvents(ctx, from, to)
			}
			latestBlock = currentBlock
		}
	}
}

// handleEvents fetches TeleportGUID events from the given block range and
// sends them to the eventCh channel.
func (ep *EventProvider) handleEvents(ctx context.Context, from, to uint64) {
	for _, address := range ep.addresses {
		ep.log.
			WithFields(log.Fields{
				"from":    from,
				"to":      to,
				"address": address.String(),
			}).
			Info("Fetching logs")
		logs, ok := ep.filterLogs(ctx, address, from, to, teleportTopic0)
		if !ok {
			return // Context was canceled.
		}
		for _, l := range logs {
			if l.Address != address {
				// This should never happen. All logs returned by
				// eth_filterLogs should be emitted by the specified
				// contract. If it happens, there is a bug somewhere.
				ep.log.
					WithFields(log.Fields{
						"expected": address.String(),
						"actual":   l.Address.String(),
					}).
					Panic("Log emitted by wrong contract")
			}
			if l.Removed {
				// This should never happen. All logs returned by
				// eth_filterLogs should not be removed.
				ep.log.
					WithFields(log.Fields{
						"address":     l.Address.String(),
						"blockNumber": l.BlockNumber,
						"blockHash":   l.BlockHash.String(),
						"txHash":      l.TxHash.String(),
					}).
					Warn("Received removed log")
				continue
			}
			evt, err := logToMessage(l)
			if err != nil {
				ep.log.
					WithError(err).
					Error("Unable to convert log to event")
				continue
			}
			ep.eventCh <- evt
		}
	}
}

// getBlockNumber returns the latest block number on the blockchain.
//
// The method will try to fetch blocks indefinitely in case of an error.
// The only way to stop this method from trying again is to cancel the
// context. In that case, the method will return false as a second return
// value.
func (ep *EventProvider) getBlockNumber(ctx context.Context) (uint64, bool) {
	var err error
	var res uint64
	retry.TryForever(
		ctx,
		func() error {
			res, err = ep.client.BlockNumber(ctx)
			if err != nil {
				ep.log.WithError(err).Error("Unable to get block number")
			}
			return err
		},
		retryInterval,
	)
	if ctx.Err() != nil {
		return 0, false
	}
	return res, true
}

// getBlockNumber returns the latest block number on the blockchain.
//
// The method will try to fetch blocks indefinitely in case of an error.
// The only way to stop this method from trying again is to cancel the
// context. In that case, the method will return false as a second return
// value.
func (ep *EventProvider) getBlockTimestamp(ctx context.Context, block uint64) (time.Time, bool) {
	var err error
	var res any
	retry.TryForever(
		ctx,
		func() error {
			res, err = ep.client.BlockByNumber(ctx, types.Uint64ToBlockNumber(block))
			if err != nil {
				ep.log.WithError(err).Error("Unable to get block timestamp")
			}
			return err
		},
		retryInterval,
	)
	if res == nil || ctx.Err() != nil {
		return time.Time{}, false
	}
	if res, ok := res.(*types.BlockTxHashes); ok {
		return time.Unix(res.Timestamp.Big().Int64(), 0), true
	}
	ep.log.Panic("BlockByNumber returned unexpected type")
	return time.Time{}, true
}

// filterLogs fetches TeleportGUID events from the blockchain.
//
// The method will try to fetch blocks indefinitely in case of an error.
// The only way to stop this method from trying again is to cancel the
// context. In that case, the method will return false as a second return
// value.
func (ep *EventProvider) filterLogs(
	ctx context.Context,
	addr types.Address,
	from, to uint64,
	topic0 types.Hash,
) ([]types.Log, bool) {

	var err error
	var res []types.Log
	retry.TryForever(
		ctx,
		func() error {
			fromBlockNumber := types.Uint64ToBlockNumber(from)
			toBlockNumber := types.Uint64ToBlockNumber(to)
			res, err = ep.client.FilterLogs(ctx, types.FilterLogsQuery{
				FromBlock: &fromBlockNumber,
				ToBlock:   &toBlockNumber,
				Address:   types.Addresses{addr},
				Topics:    []types.Hashes{{topic0}},
			})
			if err != nil {
				ep.log.WithError(err).Error("Unable to filter logs")
			}
			return err
		},
		retryInterval,
	)
	if res == nil || ctx.Err() != nil {
		return nil, false
	}
	return res, true
}

// splitBlockRanges splits a block range into smaller ranges of at most
// "limit" blocks. Some RPC providers have a limit on the number of blocks
// that can be fetched in a single request and this method is used to
// keep the number of blocks in each request below that limit.
func splitBlockRanges(from, to, limit uint64) [][2]uint64 {
	if from > to {
		return nil
	}
	if to-from <= limit {
		return [][2]uint64{{from, to}}
	}
	var ranges [][2]uint64
	rangeFrom := from
	rangeTo := from
	for rangeTo < to {
		rangeTo = rangeFrom + limit - 1
		if rangeTo > to {
			rangeTo = to
		}
		ranges = append(ranges, [2]uint64{rangeFrom, rangeTo})
		rangeFrom = rangeTo + 1
	}
	return ranges
}
