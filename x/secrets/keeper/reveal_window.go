package keeper

import (
	"context"
	"fmt"

	"github.com/timeflareio/chain/x/secrets/types"
)

// RevealWindowValidator handles progressive reveal window validation
type RevealWindowValidator struct{}

// CanGuardianRevealShare checks if a guardian can reveal a share at the current height
func (rv *RevealWindowValidator) CanGuardianRevealShare(
	ctx context.Context,
	k Keeper,
	secret types.Secret,
	guardianAddress string,
	currentHeight int64,
) error {
	// Check if we're in the reveal window.
	// Both bounds are INCLUSIVE [start, end]: a reveal in block end is valid,
	// and settlement runs in the EndBlock of block end + 1 with the final
	// revealer set. See docs/spec.md "Settlement".
	if currentHeight < secret.RevealStartBlock {
		return fmt.Errorf("reveal window not yet open: starts at block %d, current %d",
			secret.RevealStartBlock, currentHeight)
	}

	if currentHeight > secret.RevealEndBlock {
		return fmt.Errorf("reveal window closed: ended at block %d (inclusive), current %d",
			secret.RevealEndBlock, currentHeight)
	}

	// Check if guardian is assigned to this secret (an assignment record
	// exists only for guardians that were distributed a share)
	if _, err := k.GetAssignment(ctx, secret.Id, guardianAddress); err != nil {
		return fmt.Errorf("guardian %s not assigned to secret %s", guardianAddress, secret.Id)
	}

	// Check if this guardian has already revealed their share
	if k.HasGuardianRevealed(ctx, secret.Id, guardianAddress) {
		return fmt.Errorf("guardian %s already revealed share for secret %s", guardianAddress, secret.Id)
	}

	return nil
}
