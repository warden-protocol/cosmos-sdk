package vesting

import (
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
)

// LockedCoinsFromDelegating prevents the mock vesting account from delegating
// any unvested tokens.
func (mvdva MockVestedDelegateVestingAccount) LockedCoinsFromDelegating(blockTime time.Time) sdk.Coins {
	locked := mvdva.ContinuousVestingAccount.GetVestingCoins(blockTime)
	if locked == nil {
		return sdk.NewCoins()
	}

	return locked
}

func NewMockVestedDelegateVestingAccount(cva *types.ContinuousVestingAccount) *MockVestedDelegateVestingAccount {
	return &MockVestedDelegateVestingAccount{
		ContinuousVestingAccount: cva,
	}
}
