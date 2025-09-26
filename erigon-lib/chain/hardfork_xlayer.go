package chain

import (
	"fmt"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
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
	MainnetBlock uint64
	TestnetBlock uint64
	DevnetBlock  uint64
}

// Fork configurations
var ForkId13DencunConfig = XLayerForkConfig{
	MainnetBlock: 1000000000000, // TODO, need to be updated
	TestnetBlock: 7953000,
	DevnetBlock:  30,
}

// Fork configurations registry
var forkConfigs = map[ForkId]XLayerForkConfig{
	ForkId13Dencun: ForkId13DencunConfig,
	// Quickly add new fork configurations
}

// Network identification by zkevm address using common.Address type
var zkevmAddressNetworkMap = map[libcommon.Address]NetworkType{
	libcommon.HexToAddress("0x2b0ee28d4d51bc9adde5e58e295873f61f4a0507"): MainnetNetwork, // Mainnet
	libcommon.HexToAddress("0x7b1472be9a0115c3076b9f30e6bab91b13b3be6b"): TestnetNetwork, // Testnet
	libcommon.HexToAddress("0xe45ccd0757670580a4a3600de5cef1e45f0ec2bd"): LocalNetwork,   // Local
}

// Global state
var currentNetwork NetworkType = UnknownNetwork

// InitializeNetworkByZkevmAddress sets the current network based on zkevm address
// Uses common.Address type for proper address comparison
func InitializeNetworkByZkevmAddress(zkevmAddr string) {
	// Parse string to Address type (handles case-insensitive comparison automatically)
	addr := libcommon.HexToAddress(zkevmAddr)

	if network, exists := zkevmAddressNetworkMap[addr]; exists {
		currentNetwork = network
	} else {
		currentNetwork = UnknownNetwork
		log.Error(fmt.Sprintf("Unknown network: %v", zkevmAddr))
	}
	log.Info(fmt.Sprintf("Current network: %v, zkevmAddr: %v", currentNetwork, zkevmAddr))
	for key, _ := range forkConfigs {
		block := GetForkBlock(key)
		log.Info(fmt.Sprintf("Network: %v, zkevmAddr: %v, ForkId13Dencun:%v block: %v", currentNetwork, zkevmAddr, key, block))
	}
}

// GetForkBlock returns the activation block for a given fork ID
func GetForkBlock(forkID ForkId) uint64 {
	config, exists := forkConfigs[forkID]
	if !exists {
		return 0
	}

	switch currentNetwork {
	case MainnetNetwork:
		return config.MainnetBlock
	case TestnetNetwork:
		return config.TestnetBlock
	case LocalNetwork:
		return config.DevnetBlock
	default:
		return 0 // Unknown network
	}
}
