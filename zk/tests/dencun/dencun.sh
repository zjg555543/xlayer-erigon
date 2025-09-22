#!/bin/bash

# Usage: ./dencun.sh [RPC_URL] [PRIVATE_KEY] [--expect-no-hardfork]
# ./zk/tests/dencun/dencun.sh http://localhost:8123 0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2
# 
# Default values:
#   RPC_URL: http://127.0.0.1:8124
#   PRIVATE_KEY: 0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2
#   EXPECT_HARDFORK: true (expect hardfork to be active by default)
#
# Examples:
#   ./dencun.sh                                    # Use default values, expect hardfork active
#   ./dencun.sh http://localhost:8545              # Override RPC URL only
#   ./dencun.sh http://localhost:8545 0x1234...    # Override both parameters
#   ./dencun.sh --expect-no-hardfork               # Expect tests to fail (before hardfork)
#   ./dencun.sh http://localhost:8545 0x1234... --expect-no-hardfork  # All parameters

# Default values for local development
DEFAULT_RPC_URL="http://127.0.0.1:8124"
DEFAULT_PRIVATE_KEY="0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2"

# Parse arguments
RPC_URL=""
PRIVATE_KEY=""
EXPECT_HARDFORK=true

while [[ $# -gt 0 ]]; do
  case $1 in
    --expect-no-hardfork)
      EXPECT_HARDFORK=false
      shift
      ;;
    --help|-h)
      echo "Usage: $0 [RPC_URL] [PRIVATE_KEY] [--expect-no-hardfork]"
      echo ""
      echo "Default values:"
      echo "  RPC_URL: http://127.0.0.1:8124"
      echo "  PRIVATE_KEY: 0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2"
      echo "  EXPECT_HARDFORK: true (expect hardfork to be active by default)"
      echo ""
      echo "Examples:"
      echo "  $0                                    # Use default values, expect hardfork active"
      echo "  $0 http://localhost:8545              # Override RPC URL only"
      echo "  $0 http://localhost:8545 0x1234...    # Override both parameters"
      echo "  $0 --expect-no-hardfork               # Expect tests to fail (before hardfork)"
      echo "  $0 http://localhost:8545 0x1234... --expect-no-hardfork  # All parameters"
      exit 0
      ;;
    *)
      if [[ -z "$RPC_URL" ]]; then
        RPC_URL="$1"
      elif [[ -z "$PRIVATE_KEY" ]]; then
        PRIVATE_KEY="$1"
      else
        echo "Unknown option: $1" >&2
        exit 1
      fi
      shift
      ;;
  esac
done

# Use defaults if not provided
RPC_URL=${RPC_URL:-$DEFAULT_RPC_URL}
PRIVATE_KEY=${PRIVATE_KEY:-$DEFAULT_PRIVATE_KEY}

RUNDIR=$(cd "$(dirname "$0")" && pwd)
CONTRACTS_DIR="$RUNDIR/../../debug_tools/test-contracts"

echo "Using parameters:"
echo "  RPC URL: $RPC_URL"
echo "  Private Key: ${PRIVATE_KEY:0:10}..."
echo "  Expect Hardfork: $EXPECT_HARDFORK"
echo ""
echo "Current block: $(cast block-number --rpc-url $RPC_URL)"

. "$RUNDIR/../utils.sh"

# ------------------------------------
# EIP-6780: https://eips.ethereum.org/EIPS/eip-6780 Do not delete the contract
# EIP-4758: https://eips.ethereum.org/EIPS/eip-4758 Call SENDALL instead
# ------------------------------------
testSendAllEIP4758EIP6780() {
    echo "Before testSendAllEIP4758EIP6780, current block: $(cast block-number --rpc-url $RPC_URL)"
    local RPC_URL=$1
    local RECIPIENT=0x0123456789abcdef0123456789abcdef01234567
    $RUNDIR/test_selfdestruct.sh --rpc-url $RPC_URL --private-key $PRIVATE_KEY --recipient $RECIPIENT --contract $CONTRACTS_DIR/contracts/selfdestruct.sol:SelfDestruct

    if [ $? -ne 0 ]; then
        echo "SENDALL test failed."
        return 1
    fi
}

# ------------------------------------
# EIP 5656: https://eips.ethereum.org/EIPS/eip-5656 MCOPY
# ------------------------------------
testMCopyEIP5656() {
    echo "Before testMCopyEIP5656, current block: $(cast block-number --rpc-url $RPC_URL)"
    local RPC_URL=$1
    # Change to test contracts directory to avoid OpenZeppelin dependency issues
    cd $CONTRACTS_DIR
    CONTRACT=$(forge create contracts/MCopy.sol:MinimalMCopy --rpc-url $RPC_URL --private-key $PRIVATE_KEY --legacy --json --evm-version "cancun" --broadcast | jq -r '.deployedTo')
    if [ -z "$CONTRACT" ]; then
        echo "Failed to deploy MCopy contract."
        return 1
    fi

    echo "MCopy contract deployed at: $CONTRACT"

    EXPECTED_DATA="0x01020304"
    DATA=$(cast call $CONTRACT "copy(bytes)(bytes)" $EXPECTED_DATA -r $RPC_URL)

    echo "MCOPY data returned: $DATA"

    if [ "$DATA" != $EXPECTED_DATA ]; then
        if [ "$EXPECT_HARDFORK" = "true" ]; then
            echo "MCOPY data verification failed: expected $EXPECTED_DATA, got $DATA"
            echo "ERROR: Expected test to pass after hardfork, but it failed!"
            return 1
        else
            echo "MCOPY data verification failed: expected $EXPECTED_DATA, got $DATA"
            echo "EXPECTED: Test should fail before hardfork - this is correct behavior"
            return 0
        fi
    else
        if [ "$EXPECT_HARDFORK" = "true" ]; then
            echo "MCOPY data verification successful"
            echo "SUCCESS: Test passed after hardfork as expected"
            return 0
        else
            echo "MCOPY data verification successful"
            echo "ERROR: Expected test to fail before hardfork, but it passed! Hardfork might already be active."
            return 1
        fi
    fi
}

# ------------------------------------
# EIP 1153: https://eips.ethereum.org/EIPS/eip-1153 Transient storage
# ------------------------------------
testTransientStorageEIP1153() {
    echo "Before testTransientStorageEIP1153, current block: $(cast block-number --rpc-url $RPC_URL)"
    local RPC_URL=$1
    # Change to test contracts directory to avoid OpenZeppelin dependency issues
    cd $CONTRACTS_DIR
    CONTRACT=$(forge create contracts/TransientStorage.sol:TransientStorage --rpc-url $RPC_URL --private-key $PRIVATE_KEY --legacy --json --evm-version "cancun" --broadcast | jq -r '.deployedTo')
    if [ -z "$CONTRACT" ]; then
        echo "Failed to deploy transient storage contract."
        return 1
    fi

    echo "Transient storage contract deployed at: $CONTRACT"

    # Here we pick 0x010203...0004 padded to 32 bytes:
    INPUT_WORD=0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20
    DATA=$(cast call $CONTRACT "writeThenReadTransient(bytes32)(bytes32)" $INPUT_WORD -r $RPC_URL)

    echo "Transient storage data returned: $DATA"

    if [ "$DATA" != $INPUT_WORD ]; then
        if [ "$EXPECT_HARDFORK" = "true" ]; then
            echo "Transient storage data verification failed: expected $INPUT_WORD, got $DATA"
            echo "ERROR: Expected test to pass after hardfork, but it failed!"
            return 1
        else
            echo "Transient storage data verification failed: expected $INPUT_WORD, got $DATA"
            echo "EXPECTED: Test should fail before hardfork - this is correct behavior"
            return 0
        fi
    else
        if [ "$EXPECT_HARDFORK" = "true" ]; then
            echo "Transient storage data verification successful"
            echo "SUCCESS: Test passed after hardfork as expected"
            return 0
        else
            echo "Transient storage data verification successful"
            echo "ERROR: Expected test to fail before hardfork, but it passed! Hardfork might already be active."
            return 1
        fi
    fi
}

echo "=============== Running Dencun tests ==============="

run testSendAllEIP4758EIP6780 "$RPC_URL"
run testMCopyEIP5656 "$RPC_URL"
run testTransientStorageEIP1153 "$RPC_URL"

echo "=============== Dencun tests completed ==============="

