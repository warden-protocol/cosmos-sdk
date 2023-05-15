package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// initialize starting info for a new delegation
func (k Keeper) initializeDelegation(ctx sdk.Context, val sdk.ValAddress, del sdk.AccAddress) {
	// period has already been incremented - we want to store the period ended by this delegation action
	previousPeriod := k.GetValidatorCurrentRewards(ctx, val).Period - 1

	// increment reference count for the period we're going to track
	k.incrementReferenceCount(ctx, val, previousPeriod)

	validator := k.stakingKeeper.Validator(ctx, val)
	delegation := k.stakingKeeper.Delegation(ctx, del, val)

	// calculate delegation stake in tokens
	// we don't store directly, so multiply delegation shares * (tokens per share)
	// note: necessary to truncate so we don't allow withdrawing more rewards than owed
	stake := validator.TokensFromSharesTruncated(delegation.GetShares())
	k.SetDelegatorStartingInfo(ctx, val, del, types.NewDelegatorStartingInfo(previousPeriod, stake, uint64(ctx.BlockHeight())))
}

// calculate the rewards accrued by a delegation between two periods
func (k Keeper) calculateDelegationRewardsBetween(
	ctx sdk.Context,
	val stakingtypes.ValidatorI,
	startingPeriod,
	endingPeriod uint64,
	stake sdk.Dec,
) (rewards sdk.DecCoins) {
	// sanity check
	if startingPeriod > endingPeriod {
		panic("startingPeriod cannot be greater than endingPeriod")
	}

	// sanity check
	if stake.IsNegative() {
		panic("stake should not be negative")
	}

	// return staking * (ending - starting)
	starting := k.GetValidatorHistoricalRewards(ctx, val.GetOperator(), startingPeriod)
	ending := k.GetValidatorHistoricalRewards(ctx, val.GetOperator(), endingPeriod)
	difference := ending.CumulativeRewardRatio.Sub(starting.CumulativeRewardRatio)
	if difference.IsAnyNegative() {
		panic("negative rewards should not be possible")
	}
	// note: necessary to truncate so we don't allow withdrawing more rewards than owed
	rewards = difference.MulDecTruncate(stake)
	return
}

// calculate the total rewards accrued by a delegation
func (k Keeper) CalculateDelegationRewards(
	ctx sdk.Context,
	val stakingtypes.ValidatorI,
	del stakingtypes.DelegationI,
	endingPeriod uint64,
	// maxAmount max
) (rewards sdk.DecCoins) {
	validatorAddr := val.GetOperator()
	delegationAddr := del.GetDelegatorAddr()

	// fetch starting info for delegation
	startingInfo := k.GetDelegatorStartingInfo(ctx, validatorAddr, delegationAddr)

	if startingInfo.Height == uint64(ctx.BlockHeight()) {
		// started this height, no rewards yet
		return
	}

	startingPeriod := startingInfo.PreviousPeriod
	stake := startingInfo.Stake

	// Iterate through slashes and withdraw with calculated staking for
	// distribution periods. These period offsets are dependent on *when* slashes
	// happen - namely, in BeginBlock, after rewards are allocated...
	// Slashes which happened in the first block would have been before this
	// delegation existed, UNLESS they were slashes of a redelegation to this
	// validator which was itself slashed (from a fault committed by the
	// redelegation source validator) earlier in the same BeginBlock.
	startingHeight := startingInfo.Height

	// Slashes this block happened after reward allocation, but we have to account
	// for them for the stake sanity check below.
	endingHeight := uint64(ctx.BlockHeight())
	if endingHeight > startingHeight {
		k.IterateValidatorSlashEventsBetween(
			ctx, validatorAddr, startingHeight, endingHeight,
			func(height uint64, event types.ValidatorSlashEvent) (stop bool) {
				endingPeriod := event.ValidatorPeriod

				if endingPeriod > startingPeriod {

					reward := k.calculateDelegationRewardsBetween(ctx, val, startingPeriod, endingPeriod, stake)
					rewards = rewards.Add(reward...)

					// Note: It is necessary to truncate so we don't allow withdrawing
					// more rewards than owed.
					stake = stake.MulTruncate(sdk.OneDec().Sub(event.Fraction))
					startingPeriod = endingPeriod
				}
				return false
			},
		)
	}

	// A total stake sanity check; Recalculated final stake should be less than or
	// equal to current stake here. We cannot use Equals because stake is truncated
	// when multiplied by slash fractions (see above). We could only use equals if
	// we had arbitrary-precision rationals.
	currentStake := val.TokensFromShares(del.GetShares())

	if stake.GT(currentStake) {
		// AccountI for rounding inconsistencies between:
		//
		//     currentStake: calculated as in staking with a single computation
		//     stake:        calculated as an accumulation of stake
		//                   calculations across validator's distribution periods
		//
		// These inconsistencies are due to differing order of operations which
		// will inevitably have different accumulated rounding and may lead to
		// the smallest decimal place being one greater in stake than
		// currentStake. When we calculated slashing by period, even if we
		// round down for each slash fraction, it's possible due to how much is
		// being rounded that we slash less when slashing by period instead of
		// for when we slash without periods. In other words, the single slash,
		// and the slashing by period could both be rounding down but the
		// slashing by period is simply rounding down less, thus making stake >
		// currentStake
		//
		// A small amount of this error is tolerated and corrected for,
		// however any greater amount should be considered a breach in expected
		// behaviour.
		marginOfErr := sdk.SmallestDec().MulInt64(3)
		if stake.LTE(currentStake.Add(marginOfErr)) {
			stake = currentStake
		} else {
			panic(fmt.Sprintf("calculated final stake for delegator %s greater than current stake"+
				"\n\tfinal stake:\t%s"+
				"\n\tcurrent stake:\t%s",
				delegationAddr, stake, currentStake))
		}
	}

	// calculate rewards for final period
	rewards = rewards.Add(k.calculateDelegationRewardsBetween(ctx, val, startingPeriod, endingPeriod, stake)...)
	return rewards
}

func (k Keeper) withdrawDelegationRewards(
	ctx sdk.Context,
	validator stakingtypes.ValidatorI,
	delegation stakingtypes.DelegationI,
	maxAmounts sdk.Coins,
) (sdk.Coins, error) {
	validatorAddr := validator.GetOperator()
	delegatorAddr := delegation.GetDelegatorAddr()
	maxAmts := sdk.NewDecCoinsFromCoins(maxAmounts...).Sort()

	// check existence of delegator starting info
	if !k.HasDelegatorStartingInfo(ctx, validatorAddr, delegatorAddr) {
		return nil, types.ErrEmptyDelegationDistInfo
	}

	// end current period and calculate rewards
	endingPeriod := k.IncrementValidatorPeriod(ctx, validator)
	rewardsRaw := k.CalculateDelegationRewards(ctx, validator, delegation, endingPeriod)
	outstanding := k.GetValidatorOutstandingRewardsCoins(ctx, validatorAddr)

	// defensive edge case may happen on the very final digits
	// of the decCoins due to operation order of the distribution mechanism.
	rewards := rewardsRaw.Intersect(outstanding)
	if !rewards.IsEqual(rewardsRaw) {
		logger := k.Logger(ctx)
		logger.Info(
			"rounding error withdrawing rewards from validator",
			"delegator", delegatorAddr.String(),
			"validator", validatorAddr.String(),
			"got", rewards.String(),
			"expected", rewardsRaw.String(),
		)
	}

	// allocate the rewards to the DelegatorOutstandingRewards
	outstanding := k.DelegationOutstandingRewards(ctx, delegatorAddr, validatorAddr)
	outstanding = outstanding.Add(rewards...)

	outstandingCpy := make(sdk.DecCoins, 0)
	copy(outstandingCpy, outstanding)

	// update the outstanding rewards by substracting max(maxAmt, outstanding)
	var claimedRewards sdk.DecCoins
	for _, decCoin := range outstanding {
		maxAmt := sdk.MaxDec(decCoin.Amount, maxAmts.AmountOf(decCoin.Denom))
		claimedRewards = claimedRewards.Add(sdk.NewDecCoinFromDec(decCoin.Denom, maxAmt))
	}

	// maxDec := sdk.NewDecCoinsFromCoins(maxAmount...)

	// truncate reward dec coins, return remainder to community pool
	finalRewards, remainder := claimedRewards.TruncateDecimal()

	// add coins to user account
	if !finalRewards.IsZero() {
		withdrawAddr := k.GetDelegatorWithdrawAddr(ctx, delegatorAddr)
		err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, withdrawAddr, finalRewards)
		if err != nil {
			return nil, err
		}
	}

	// update the outstanding rewards and the community pool only if the
	// transaction was successful
	k.SetValidatorOutstandingRewards(ctx, validatorAddr, types.ValidatorOutstandingRewards{Rewards: outstanding.Sub(rewards)})
	// TODO:
	// k.SetDelegationOutstandingRewards(ctx, delegatorAddr, validatorAddr, outstanding)
	feePool := k.GetFeePool(ctx)
	feePool.CommunityPool = feePool.CommunityPool.Add(remainder...)
	k.SetFeePool(ctx, feePool)

	// decrement reference count of starting period
	startingInfo := k.GetDelegatorStartingInfo(ctx, validatorAddr, delegatorAddr)
	startingPeriod := startingInfo.PreviousPeriod
	k.decrementReferenceCount(ctx, validatorAddr, startingPeriod)

	// remove delegator starting info
	k.DeleteDelegatorStartingInfo(ctx, validatorAddr, delegatorAddr)

	emittedRewards := finalRewards
	if finalRewards.IsZero() {
		baseDenom, _ := sdk.GetBaseDenom()
		if baseDenom == "" {
			baseDenom = sdk.DefaultBondDenom
		}

		// Note, we do not call the NewCoins constructor as we do not want the zero
		// coin removed.
		emittedRewards = sdk.Coins{sdk.NewCoin(baseDenom, sdk.ZeroInt())}
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeWithdrawRewards,
			sdk.NewAttribute(sdk.AttributeKeyAmount, emittedRewards.String()),
			sdk.NewAttribute(types.AttributeKeyValidator, validatorAddr.String()),
		),
	)

	return finalRewards, nil
}
