package chain

import (
	"testing"
)

// Test NetworkType constants
func TestNetworkTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		network  NetworkType
		expected int
	}{
		{"UnknownNetwork", UnknownNetwork, 0},
		{"MainnetNetwork", MainnetNetwork, 1},
		{"TestnetNetwork", TestnetNetwork, 2},
		{"LocalNetwork", LocalNetwork, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.network) != tt.expected {
				t.Errorf("NetworkType %s = %d, want %d", tt.name, int(tt.network), tt.expected)
			}
		})
	}
}

// Test XLayerForkConfig struct
func TestXLayerForkConfig(t *testing.T) {
	config := XLayerForkConfig{
		MainnetBlock: 100,
		TestnetBlock: 200,
		DevnetBlock:  300,
	}

	if config.MainnetBlock != 100 {
		t.Errorf("MainnetBlock = %d, want 100", config.MainnetBlock)
	}
	if config.TestnetBlock != 200 {
		t.Errorf("TestnetBlock = %d, want 200", config.TestnetBlock)
	}
	if config.DevnetBlock != 300 {
		t.Errorf("DevnetBlock = %d, want 300", config.DevnetBlock)
	}
}

// Test ForkId13DencunConfig initialization
func TestForkId13DencunConfig(t *testing.T) {
	config := ForkId13DencunConfig

	if config.MainnetBlock != 1000000000000 {
		t.Errorf("ForkId13DencunConfig.MainnetBlock = %d, want 1000000000000", config.MainnetBlock)
	}
	if config.TestnetBlock != 1000000000000 {
		t.Errorf("ForkId13DencunConfig.TestnetBlock = %d, want 1000000000000", config.TestnetBlock)
	}
	if config.DevnetBlock != 20 {
		t.Errorf("ForkId13DencunConfig.DevnetBlock = %d, want 20", config.DevnetBlock)
	}
}

// Test forkConfigs registry
func TestForkConfigsRegistry(t *testing.T) {
	// Test that ForkId13Dencun exists in the registry
	config, exists := forkConfigs[ForkId13Dencun]
	if !exists {
		t.Error("ForkId13Dencun should exist in forkConfigs registry")
	}

	// Verify the config values
	if config.MainnetBlock != 1000000000000 {
		t.Errorf("forkConfigs[ForkId13Dencun].MainnetBlock = %d, want 1000000000000", config.MainnetBlock)
	}
	if config.TestnetBlock != 1000000000000 {
		t.Errorf("forkConfigs[ForkId13Dencun].TestnetBlock = %d, want 1000000000000", config.TestnetBlock)
	}
	if config.DevnetBlock != 20 {
		t.Errorf("forkConfigs[ForkId13Dencun].DevnetBlock = %d, want 20", config.DevnetBlock)
	}
}

// Test zkevmAddressNetworkMap
func TestZkevmAddressNetworkMap(t *testing.T) {
	tests := []struct {
		name    string
		address string
		network NetworkType
	}{
		{"Mainnet", "0x2B0ee28D4D51bC9aDde5E58E295873F61F4a0507", MainnetNetwork},
		{"Testnet", "0x7b1472be9a0115c3076b9f30e6bab91b13b3be6b", TestnetNetwork},
		{"Local", "0xE45CCD0757670580a4a3600DE5cef1e45F0Ec2bd", LocalNetwork},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network, exists := zkevmAddressNetworkMap[tt.address]
			if !exists {
				t.Errorf("Address %s should exist in zkevmAddressNetworkMap", tt.address)
			}
			if network != tt.network {
				t.Errorf("zkevmAddressNetworkMap[%s] = %d, want %d", tt.address, network, tt.network)
			}
		})
	}
}

// Test InitializeNetworkByZkevmAddress function
func TestInitializeNetworkByZkevmAddress(t *testing.T) {
	// Save original state
	originalNetwork := currentNetwork
	defer func() {
		currentNetwork = originalNetwork
	}()

	tests := []struct {
		name            string
		zkevmAddr       string
		expectedNetwork NetworkType
	}{
		{"Mainnet Address", "0x2B0ee28D4D51bC9aDde5E58E295873F61F4a0507", MainnetNetwork},
		{"Testnet Address", "0x7b1472be9a0115c3076b9f30e6bab91b13b3be6b", TestnetNetwork},
		{"Local Address", "0xE45CCD0757670580a4a3600DE5cef1e45F0Ec2bd", LocalNetwork},
		{"Unknown Address", "0x1234567890123456789012345678901234567890", UnknownNetwork},
		{"Empty Address", "", UnknownNetwork},
		{"Invalid Address", "invalid_address", UnknownNetwork},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			InitializeNetworkByZkevmAddress(tt.zkevmAddr)
			if currentNetwork != tt.expectedNetwork {
				t.Errorf("InitializeNetworkByZkevmAddress(%s): currentNetwork = %d, want %d",
					tt.zkevmAddr, currentNetwork, tt.expectedNetwork)
			}
		})
	}
}

// Test GetForkBlock function
func TestGetForkBlock(t *testing.T) {
	// Save original state
	originalNetwork := currentNetwork
	defer func() {
		currentNetwork = originalNetwork
	}()

	tests := []struct {
		name          string
		network       NetworkType
		forkID        ForkId
		expectedBlock uint64
	}{
		{"Mainnet ForkId13Dencun", MainnetNetwork, ForkId13Dencun, 1000000000000},
		{"Testnet ForkId13Dencun", TestnetNetwork, ForkId13Dencun, 1000000000000},
		{"Local ForkId13Dencun", LocalNetwork, ForkId13Dencun, 20},
		{"Unknown Network ForkId13Dencun", UnknownNetwork, ForkId13Dencun, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentNetwork = tt.network
			block := GetForkBlock(tt.forkID)
			if block != tt.expectedBlock {
				t.Errorf("GetForkBlock(%d) with network %d = %d, want %d",
					tt.forkID, tt.network, block, tt.expectedBlock)
			}
		})
	}
}

// Test GetForkBlock with non-existent fork ID
func TestGetForkBlockNonExistentFork(t *testing.T) {
	// Save original state
	originalNetwork := currentNetwork
	defer func() {
		currentNetwork = originalNetwork
	}()

	// Test with a fork ID that doesn't exist in forkConfigs
	currentNetwork = MainnetNetwork
	block := GetForkBlock(ForkID4) // ForkID4 is not in forkConfigs
	if block != 0 {
		t.Errorf("GetForkBlock for non-existent fork should return 0, got %d", block)
	}
}

// Test concurrent access to global state (basic thread safety test)
func TestConcurrentNetworkInitialization(t *testing.T) {
	// Save original state
	originalNetwork := currentNetwork
	defer func() {
		currentNetwork = originalNetwork
	}()

	addresses := []string{
		"0x2B0ee28D4D51bC9aDde5E58E295873F61F4a0507",
		"0x7b1472be9a0115c3076b9f30e6bab91b13b3be6b",
		"0xE45CCD0757670580a4a3600DE5cef1e45F0Ec2bd",
	}

	// Test concurrent initialization
	done := make(chan bool, len(addresses))
	for _, addr := range addresses {
		go func(address string) {
			InitializeNetworkByZkevmAddress(address)
			done <- true
		}(addr)
	}

	// Wait for all goroutines to complete
	for i := 0; i < len(addresses); i++ {
		<-done
	}

	// Verify that currentNetwork is set to one of the valid networks
	validNetworks := []NetworkType{MainnetNetwork, TestnetNetwork, LocalNetwork}
	isValid := false
	for _, validNetwork := range validNetworks {
		if currentNetwork == validNetwork {
			isValid = true
			break
		}
	}

	if !isValid {
		t.Errorf("After concurrent initialization, currentNetwork should be one of valid networks, got %d", currentNetwork)
	}
}

// Test edge cases for address format
func TestInitializeNetworkByZkevmAddressEdgeCases(t *testing.T) {
	// Save original state
	originalNetwork := currentNetwork
	defer func() {
		currentNetwork = originalNetwork
	}()

	tests := []struct {
		name            string
		zkevmAddr       string
		expectedNetwork NetworkType
	}{
		{"Lowercase mainnet", "0x2b0ee28d4d51bc9adde5e58e295873f61f4a0507", UnknownNetwork}, // Case sensitive
		{"Uppercase mainnet", "0X2B0EE28D4D51BC9ADDE5E58E295873F61F4A0507", UnknownNetwork}, // Case sensitive
		{"Without 0x prefix", "2B0ee28D4D51bC9aDde5E58E295873F61F4a0507", UnknownNetwork},
		{"With extra characters", "0x2B0ee28D4D51bC9aDde5E58E295873F61F4a0507x", UnknownNetwork},
		{"Short address", "0x2B0ee28D4D51bC9aDde5E58E295873F61F4a050", UnknownNetwork},
		{"Long address", "0x2B0ee28D4D51bC9aDde5E58E295873F61F4a05077", UnknownNetwork},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			InitializeNetworkByZkevmAddress(tt.zkevmAddr)
			if currentNetwork != tt.expectedNetwork {
				t.Errorf("InitializeNetworkByZkevmAddress(%s): currentNetwork = %d, want %d",
					tt.zkevmAddr, currentNetwork, tt.expectedNetwork)
			}
		})
	}
}

// Test GetForkBlock with all network types
func TestGetForkBlockAllNetworks(t *testing.T) {
	// Save original state
	originalNetwork := currentNetwork
	defer func() {
		currentNetwork = originalNetwork
	}()

	networks := []NetworkType{UnknownNetwork, MainnetNetwork, TestnetNetwork, LocalNetwork}

	for _, network := range networks {
		t.Run(string(rune(network+'0')), func(t *testing.T) {
			currentNetwork = network
			block := GetForkBlock(ForkId13Dencun)

			var expectedBlock uint64
			switch network {
			case MainnetNetwork:
				expectedBlock = 1000000000000
			case TestnetNetwork:
				expectedBlock = 1000000000000
			case LocalNetwork:
				expectedBlock = 20
			default:
				expectedBlock = 0
			}

			if block != expectedBlock {
				t.Errorf("GetForkBlock(ForkId13Dencun) with network %d = %d, want %d",
					network, block, expectedBlock)
			}
		})
	}
}

// Benchmark tests for performance evaluation
func BenchmarkInitializeNetworkByZkevmAddress(b *testing.B) {
	// Save original state
	originalNetwork := currentNetwork
	defer func() {
		currentNetwork = originalNetwork
	}()

	addresses := []string{
		"0x2B0ee28D4D51bC9aDde5E58E295873F61F4a0507", // Mainnet
		"0x7b1472be9a0115c3076b9f30e6bab91b13b3be6b", // Testnet
		"0xE45CCD0757670580a4a3600DE5cef1e45F0Ec2bd", // Local
		"0x1234567890123456789012345678901234567890", // Unknown
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		addr := addresses[i%len(addresses)]
		InitializeNetworkByZkevmAddress(addr)
	}
}

func BenchmarkGetForkBlock(b *testing.B) {
	// Save original state
	originalNetwork := currentNetwork
	defer func() {
		currentNetwork = originalNetwork
	}()

	// Set to mainnet for consistent benchmarking
	currentNetwork = MainnetNetwork

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetForkBlock(ForkId13Dencun)
	}
}

// Test NetworkType string representation (if needed for debugging)
func TestNetworkTypeString(t *testing.T) {
	// This test demonstrates how network types can be used for string representation
	networks := map[NetworkType]string{
		UnknownNetwork: "Unknown",
		MainnetNetwork: "Mainnet",
		TestnetNetwork: "Testnet",
		LocalNetwork:   "Local",
	}

	for network, expectedStr := range networks {
		// Since NetworkType doesn't have a String() method, we test the concept
		var actualStr string
		switch network {
		case UnknownNetwork:
			actualStr = "Unknown"
		case MainnetNetwork:
			actualStr = "Mainnet"
		case TestnetNetwork:
			actualStr = "Testnet"
		case LocalNetwork:
			actualStr = "Local"
		default:
			actualStr = "Invalid"
		}

		if actualStr != expectedStr {
			t.Errorf("NetworkType(%d) string representation = %s, want %s", network, actualStr, expectedStr)
		}
	}
}

// Test XLayerForkConfig zero values
func TestXLayerForkConfigZeroValues(t *testing.T) {
	var config XLayerForkConfig

	if config.MainnetBlock != 0 {
		t.Errorf("Zero value XLayerForkConfig.MainnetBlock = %d, want 0", config.MainnetBlock)
	}
	if config.TestnetBlock != 0 {
		t.Errorf("Zero value XLayerForkConfig.TestnetBlock = %d, want 0", config.TestnetBlock)
	}
	if config.DevnetBlock != 0 {
		t.Errorf("Zero value XLayerForkConfig.DevnetBlock = %d, want 0", config.DevnetBlock)
	}
}

// Test multiple fork configurations (future-proof test)
func TestMultipleForkConfigurations(t *testing.T) {
	// Save original state
	originalNetwork := currentNetwork
	defer func() {
		currentNetwork = originalNetwork
	}()

	// Test that we can handle multiple fork configurations
	currentNetwork = MainnetNetwork

	// Test existing fork
	block := GetForkBlock(ForkId13Dencun)
	if block != 1000000000000 {
		t.Errorf("GetForkBlock(ForkId13Dencun) = %d, want 1000000000000", block)
	}

	// Test non-existent fork (should return 0)
	block = GetForkBlock(ForkID4)
	if block != 0 {
		t.Errorf("GetForkBlock(ForkID4) for non-existent fork = %d, want 0", block)
	}
}

// Test address map immutability (defensive programming)
func TestZkevmAddressNetworkMapImmutability(t *testing.T) {
	// Ensure the original map is not modified during tests
	expectedSize := 3
	if len(zkevmAddressNetworkMap) != expectedSize {
		t.Errorf("zkevmAddressNetworkMap size = %d, want %d", len(zkevmAddressNetworkMap), expectedSize)
	}

	// Check all expected addresses exist
	expectedAddresses := []string{
		"0x2B0ee28D4D51bC9aDde5E58E295873F61F4a0507",
		"0x7b1472be9a0115c3076b9f30e6bab91b13b3be6b",
		"0xE45CCD0757670580a4a3600DE5cef1e45F0Ec2bd",
	}

	for _, addr := range expectedAddresses {
		if _, exists := zkevmAddressNetworkMap[addr]; !exists {
			t.Errorf("Expected address %s not found in zkevmAddressNetworkMap", addr)
		}
	}
}

// Test fork configs registry immutability
func TestForkConfigsRegistryImmutability(t *testing.T) {
	// Ensure the fork configs registry contains expected entries
	expectedSize := 1 // Currently only ForkId13Dencun
	if len(forkConfigs) != expectedSize {
		t.Errorf("forkConfigs size = %d, want %d", len(forkConfigs), expectedSize)
	}

	// Verify ForkId13Dencun exists
	if _, exists := forkConfigs[ForkId13Dencun]; !exists {
		t.Error("ForkId13Dencun should exist in forkConfigs")
	}
}

// Test state restoration after network changes
func TestNetworkStateRestoration(t *testing.T) {
	// Save original state
	originalNetwork := currentNetwork
	defer func() {
		currentNetwork = originalNetwork
	}()

	// Change network multiple times
	InitializeNetworkByZkevmAddress("0x2B0ee28D4D51bC9aDde5E58E295873F61F4a0507") // Mainnet
	if currentNetwork != MainnetNetwork {
		t.Errorf("After mainnet initialization: currentNetwork = %d, want %d", currentNetwork, MainnetNetwork)
	}

	InitializeNetworkByZkevmAddress("0x7b1472be9a0115c3076b9f30e6bab91b13b3be6b") // Testnet
	if currentNetwork != TestnetNetwork {
		t.Errorf("After testnet initialization: currentNetwork = %d, want %d", currentNetwork, TestnetNetwork)
	}

	InitializeNetworkByZkevmAddress("unknown_address") // Unknown
	if currentNetwork != UnknownNetwork {
		t.Errorf("After unknown initialization: currentNetwork = %d, want %d", currentNetwork, UnknownNetwork)
	}
}
