package vesting

import (
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
)

// LockedCoinsFromDelegating prevents the mock vesting account from delegating
// any unvested tokens.
func (mvdva MockVestedDelegateVestingAccount) LockedCoinsFromDelegating(blockTime time.Time) sdk.Coins {
	return mvdva.ContinuousVestingAccount.GetVestingCoins(blockTime)
}

func NewMockVestedDelegateVestingAccount(cva *types.ContinuousVestingAccount) *MockVestedDelegateVestingAccount {
	return &MockVestedDelegateVestingAccount{
		ContinuousVestingAccount: cva,
	}
}
