package chain

import (
	"fmt"

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
	TestnetBlock: 1000000000000, // TODO, need to be updated
	DevnetBlock:  10,
}

// Fork configurations registry
var forkConfigs = map[ForkId]XLayerForkConfig{
	ForkId13Dencun: ForkId13DencunConfig,
	// Quickly add new fork configurations
}

// Network identification by zkevm address
var zkevmAddressNetworkMap = map[string]NetworkType{
	"0x2B0ee28D4D51bC9aDde5E58E295873F61F4a0507": MainnetNetwork, // Mainnet
	"0x7b1472be9a0115c3076b9f30e6bab91b13b3be6b": TestnetNetwork, // Testnet
	"0xE45CCD0757670580a4a3600DE5cef1e45F0Ec2bd": LocalNetwork,   // Local
}

// Global state
var currentNetwork NetworkType = UnknownNetwork

// InitializeNetworkByZkevmAddress sets the current network based on zkevm address
func InitializeNetworkByZkevmAddress(zkevmAddr string) {
	if network, exists := zkevmAddressNetworkMap[zkevmAddr]; exists {
		currentNetwork = network
	} else {
		currentNetwork = UnknownNetwork
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
