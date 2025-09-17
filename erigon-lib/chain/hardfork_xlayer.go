/*
   Copyright 2021 Erigon contributors

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package chain

import (
	"fmt"
	"math/big"

	"github.com/ledgerwatch/log/v3"
)

// NetworkType represents different X Layer network environments
type NetworkType int

const (
	UnknownNetwork NetworkType = iota
	MainnetNetwork
	TestnetNetwork
	LocalNetwork
)

// XLayerForkConfig defines hard fork activation blocks for different networks
type XLayerForkConfig struct {
	MainnetBlock *big.Int
	TestnetBlock *big.Int
	DevnetBlock  *big.Int
}

// Fork configurations
var ForkId13DurianDencunConfig = XLayerForkConfig{
	MainnetBlock: big.NewInt(5000000),
	TestnetBlock: big.NewInt(3000000),
	DevnetBlock:  big.NewInt(100),
}

// Fork configurations registry
var forkConfigs = map[ForkId]XLayerForkConfig{
	ForkId13DurianDencun: ForkId13DurianDencunConfig,
	// Quickly add new fork configurations
}

// Network identification by sequencer address
var zkevmAddressNetworkMap = map[string]NetworkType{
	"0x2B0ee28D4D51bC9aDde5E58E295873F61F4a0507": MainnetNetwork, // X Layer mainnet sequencer
	"0x7b1472be9a0115c3076b9f30e6bab91b13b3be6b": TestnetNetwork, // X Layer testnet sequencer
	"0xE45CCD0757670580a4a3600DE5cef1e45F0Ec2bd": LocalNetwork,   // Local sequencer
}

// Global state
var currentNetwork NetworkType = UnknownNetwork

// InitializeNetworkByZkevmAddress sets the current network based on zkevm address
func InitializeNetworkByZkevmAddress(sequencerAddr string) {
	if network, exists := zkevmAddressNetworkMap[sequencerAddr]; exists {
		currentNetwork = network
	} else {
		currentNetwork = UnknownNetwork
	}
	for key, _ := range forkConfigs {
		block := GetForkBlock(key)
		log.Info(fmt.Sprintf("Current network: %v, sequencerAddr: %v, ForkId13DurianDencun:%v block: %v", currentNetwork, sequencerAddr, key, block))
	}
}

// RegisterFork allows registering new fork configurations dynamically
func RegisterFork(forkID ForkId, config XLayerForkConfig) {
	forkConfigs[forkID] = config
}

// GetForkBlock returns the activation block for a given fork ID
func GetForkBlock(forkID ForkId) *big.Int {
	config, exists := forkConfigs[forkID]
	if !exists {
		return nil
	}

	switch currentNetwork {
	case MainnetNetwork:
		return config.MainnetBlock
	case TestnetNetwork:
		return config.TestnetBlock
	case LocalNetwork:
		return config.DevnetBlock
	default:
		return nil // Unknown network
	}
}
