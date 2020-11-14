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

package geth

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"

	internalEthereum "github.com/makerdao/gofer/internal/ethereum"
)

const (
	mainnetChainID = 1
	kovanChainID   = 42
	rinkebyChainID = 4
	gorliChainID   = 5
	ropstenChainID = 3
	xdaiChainID    = 100
)

// Addresses of multicall contracts. They're used to implement
// the Client.MultiCall function.
//
// https://github.com/makerdao/multicall
var multiCallContracts = map[uint64]common.Address{
	mainnetChainID: common.HexToAddress("0xeefba1e63905ef1d7acba5a8513c70307c1ce441"),
	kovanChainID:   common.HexToAddress("0x2cc8688c5f75e365aaeeb4ea8d6a480405a48d2a"),
	rinkebyChainID: common.HexToAddress("0x42ad527de7d4e9d9d011ac45b31d8551f8fe9821"),
	gorliChainID:   common.HexToAddress("0x77dca2c955b15e9de4dbbcf1246b4b85b651e50e"),
	ropstenChainID: common.HexToAddress("0x53c43764255c17bd724f74c4ef150724ac50a3ed"),
	xdaiChainID:    common.HexToAddress("0xb5b692a88bdfc81ca69dcb1d924f59f0413a602a"),
}

// RevertErr may be returned by Client.Call method in case of EVM revert.
type RevertErr struct {
	Message string
	Err     error
}

func (e RevertErr) Error() string {
	return fmt.Sprintf("reverted: %s", e.Message)
}

func (e RevertErr) Unwrap() error {
	return e.Err
}

// EthClient represents the Ethereum client, like the ethclient.Client.
type EthClient interface {
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error)
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
	NetworkID(ctx context.Context) (*big.Int, error)
}

// Client implements the ethereum.Client interface.
type Client struct {
	ethClient EthClient
	signer    internalEthereum.Signer
}

// NewClient returns a new Client instance.
func NewClient(ethClient EthClient, signer internalEthereum.Signer) *Client {
	return &Client{
		ethClient: ethClient,
		signer:    signer,
	}
}

// Call implements the ethereum.Client interface.
func (e *Client) Call(ctx context.Context, call internalEthereum.Call) ([]byte, error) {
	cm := ethereum.CallMsg{
		From:     e.signer.Address(),
		To:       &call.Address,
		Gas:      0,
		GasPrice: nil,
		Value:    nil,
		Data:     call.Data,
	}

	resp, err := e.ethClient.CallContract(ctx, cm, nil)
	if err := isRevertErr(err); err != nil {
		return nil, err
	}
	if err := isRevertResp(resp); err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}

	return resp, err
}

// MultiCall implements the ethereum.Client interface.
func (e *Client) MultiCall(ctx context.Context, calls []internalEthereum.Call) ([][]byte, error) {
	type abiCall struct {
		Address common.Address `abi:"target"`
		Data    []byte         `abi:"callData"`
	}
	var abiCalls []abiCall
	for _, c := range calls {
		abiCalls = append(abiCalls, abiCall{
			Address: c.Address,
			Data:    c.Data,
		})
	}

	chainID, err := e.ethClient.NetworkID(ctx)
	if err != nil {
		return nil, err
	}
	multicallAddr, ok := multiCallContracts[chainID.Uint64()]
	if !ok {
		return nil, errors.New("multi call is not supported on current chain")
	}
	cd, err := multiCallABI.Pack("aggregate", abiCalls)
	if err != nil {
		return nil, err
	}
	resp, err := e.Call(ctx, internalEthereum.Call{Address: multicallAddr, Data: cd})
	if err != nil {
		return nil, err
	}
	results, err := multiCallABI.Unpack("aggregate", resp)
	if err != nil {
		return nil, err
	}

	return results[1].([][]byte), nil
}

// Storage implements the ethereum.Client interface.
func (e *Client) Storage(ctx context.Context, address internalEthereum.Address, key internalEthereum.Hash) ([]byte, error) {
	return e.ethClient.StorageAt(ctx, address, key, nil)
}

// SendTransaction implements the ethereum.Client interface.
func (e *Client) SendTransaction(ctx context.Context, transaction *internalEthereum.Transaction) (*internalEthereum.Hash, error) {
	var err error

	// We don't want to modify passed structure because that would be rude, so
	// we copy it here:
	tx := &internalEthereum.Transaction{
		Address:  transaction.Address,
		Nonce:    transaction.Nonce,
		Gas:      transaction.Gas,
		GasLimit: transaction.GasLimit,
		ChainID:  transaction.ChainID,
		SignedTx: transaction.SignedTx,
	}
	tx.Data = make([]byte, len(transaction.Data))
	copy(tx.Data, transaction.Data)

	// Fill optional values if necessary:
	if tx.Nonce == 0 {
		tx.Nonce, err = e.ethClient.PendingNonceAt(ctx, e.signer.Address())
		if err != nil {
			return nil, err
		}
	}
	if tx.Gas == nil {
		tx.Gas, err = e.ethClient.SuggestGasPrice(ctx)
		if err != nil {
			return nil, err
		}
	}
	if tx.ChainID == nil {
		tx.ChainID, err = e.ethClient.NetworkID(ctx)
		if err != nil {
			return nil, err
		}
	}
	if tx.SignedTx == nil {
		err = e.signer.SignTransaction(tx)
		if err != nil {
			return nil, err
		}
	}

	// Send transaction:
	if stx, ok := tx.SignedTx.(*types.Transaction); ok {
		hash := stx.Hash()
		return &hash, e.ethClient.SendTransaction(ctx, stx)
	}
	return nil, errors.New("unable to send transaction, SignedTx field have invalid type")
}

func isRevertResp(resp []byte) error {
	revert, err := abi.UnpackRevert(resp)
	if err != nil {
		return nil
	}

	return RevertErr{Message: revert, Err: nil}
}

func isRevertErr(vmErr error) error {
	switch terr := vmErr.(type) {
	case rpc.DataError:
		// Some RPC servers returns "revert" data as a hex encoded string, here
		// we're trying to parse it:
		if str, ok := terr.ErrorData().(string); ok {
			re := regexp.MustCompile("(0x[a-zA-Z0-9]+)")
			match := re.FindStringSubmatch(str)

			if len(match) == 2 && len(match[1]) > 2 {
				bytes, err := hex.DecodeString(match[1][2:])
				if err != nil {
					return nil
				}

				revert, err := abi.UnpackRevert(bytes)
				if err != nil {
					return nil
				}

				return RevertErr{Message: revert, Err: vmErr}
			}
		}
	}

	return nil
}
