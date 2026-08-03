package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/looplab/fsm"

	"github.com/timeflareio/chain/x/secrets/types"
)

// SecretStateMachine manages the state transitions for secrets using a formal FSM
// This replaces ad-hoc state checking with a proper state machine implementation
type SecretStateMachine struct {
	fsm      *fsm.FSM
	secretId string
	keeper   Keeper
	ctx      context.Context
}

// State transition events
const (
	EventSharesDistributed   = "shares_distributed"
	EventSufficientAccepted  = "sufficient_accepted"
	EventRevealWindowStarted = "reveal_window_started"
	EventThresholdReached    = "threshold_reached"
	EventWindowClosed        = "window_closed"
	EventSecretCancelled     = "secret_cancelled"
	EventAcceptanceTimeout   = "acceptance_timeout"
	EventDistributionTimeout = "distribution_timeout"
	EventRevealTimeout       = "reveal_timeout"
)

// NewSecretStateMachine creates a new FSM for managing secret lifecycle transitions
func NewSecretStateMachine(secretId string, initialState string, keeper Keeper, ctx context.Context) *SecretStateMachine {
	sm := &SecretStateMachine{
		secretId: secretId,
		keeper:   keeper,
		ctx:      ctx,
	}

	sm.fsm = fsm.NewFSM(
		initialState,
		fsm.Events{
			// Phase 2: Distribution -> awaiting acceptance
			{Name: EventSharesDistributed, Src: []string{types.SECRET_STATUS_RESERVED}, Dst: types.SECRET_STATUS_AWAITING_ACCEPTANCE},

			// Phase 3: Acceptance -> pending (active secret)
			{Name: EventSufficientAccepted, Src: []string{types.SECRET_STATUS_AWAITING_ACCEPTANCE}, Dst: types.SECRET_STATUS_PENDING},

			// Reveal phase: Pending -> reconstructable (threshold reached, window still open)
			{Name: EventThresholdReached, Src: []string{types.SECRET_STATUS_PENDING}, Dst: types.SECRET_STATUS_RECONSTRUCTABLE},

			// Window closure: Reconstructable -> revealed (final state)
			{Name: EventWindowClosed, Src: []string{types.SECRET_STATUS_RECONSTRUCTABLE}, Dst: types.SECRET_STATUS_REVEALED},

			// Cancellation: pending -> cancelled ONLY. Cancellation is a
			// post-activation mechanic (the paid pro-rata exit for bonded
			// guardians); pre-activation secrets exit via commit-timeout.
			{Name: EventSecretCancelled, Src: []string{
				types.SECRET_STATUS_PENDING,
			}, Dst: types.SECRET_STATUS_CANCELLED},

			// Failure states: Timeouts -> failed
			{Name: EventDistributionTimeout, Src: []string{types.SECRET_STATUS_RESERVED}, Dst: types.SECRET_STATUS_FAILED},
			{Name: EventAcceptanceTimeout, Src: []string{types.SECRET_STATUS_AWAITING_ACCEPTANCE}, Dst: types.SECRET_STATUS_FAILED},
			{Name: EventRevealTimeout, Src: []string{types.SECRET_STATUS_PENDING}, Dst: types.SECRET_STATUS_FAILED},
		},
		fsm.Callbacks{
			"before_event": func(ctx context.Context, e *fsm.Event) {
				sm.beforeTransition(e)
			},
			"after_" + EventSharesDistributed: func(ctx context.Context, e *fsm.Event) {
				sm.afterSharesDistributed(e)
			},
			"after_" + EventSufficientAccepted: func(ctx context.Context, e *fsm.Event) {
				sm.afterSufficientAccepted(e)
			},
			"after_" + EventThresholdReached: func(ctx context.Context, e *fsm.Event) {
				sm.afterThresholdReached(e)
			},
			"after_" + EventWindowClosed: func(ctx context.Context, e *fsm.Event) {
				sm.afterWindowClosed(e)
			},
			"after_" + EventSecretCancelled: func(ctx context.Context, e *fsm.Event) {
				sm.afterSecretCancelled(e)
			},
			"after_" + EventDistributionTimeout: func(ctx context.Context, e *fsm.Event) {
				sm.afterDistributionTimeout(e)
			},
			"after_" + EventAcceptanceTimeout: func(ctx context.Context, e *fsm.Event) {
				sm.afterAcceptanceTimeout(e)
			},
			"after_" + EventRevealTimeout: func(ctx context.Context, e *fsm.Event) {
				sm.afterRevealTimeout(e)
			},
		},
	)

	return sm
}

// TransitionTo attempts to transition the secret to a new state via the specified event
func (sm *SecretStateMachine) TransitionTo(event string) error {
	if err := sm.fsm.Event(sm.ctx, event); err != nil {
		return fmt.Errorf("invalid state transition from %s via %s: %w", sm.fsm.Current(), event, err)
	}
	return nil
}

// CanTransition checks if a transition is valid without performing it
func (sm *SecretStateMachine) CanTransition(event string) bool {
	return sm.fsm.Can(event)
}

// CurrentState returns the current state of the secret
func (sm *SecretStateMachine) CurrentState() string {
	return sm.fsm.Current()
}

// GetAllowedTransitions returns all valid transitions from the current state
func (sm *SecretStateMachine) GetAllowedTransitions() []string {
	return sm.fsm.AvailableTransitions()
}

// FSM Callback Functions

func (sm *SecretStateMachine) beforeTransition(e *fsm.Event) {
	sdkCtx := sdk.UnwrapSDKContext(sm.ctx)
	sdkCtx.Logger().Info("Secret state transition",
		"secret_id", sm.secretId,
		"from", e.Src,
		"to", e.Dst,
		"event", e.Event,
		"block_height", sdkCtx.BlockHeight(),
	)
}

func (sm *SecretStateMachine) afterSharesDistributed(e *fsm.Event) {
	sm.emitStateTransitionEvent(types.EventTypeSecretAwaitingAcceptance, "encrypted shares distributed to guardians")
}

func (sm *SecretStateMachine) afterSufficientAccepted(e *fsm.Event) {
	sm.emitStateTransitionEvent(types.EventTypeSecretPending, "sufficient guardians accepted, secret is now pending")
}

func (sm *SecretStateMachine) afterThresholdReached(e *fsm.Event) {
	sm.emitStateTransitionEvent(types.EventTypeSecretReconstructable, "threshold reached, secret is reconstructable")
}

func (sm *SecretStateMachine) afterWindowClosed(e *fsm.Event) {
	sm.emitStateTransitionEvent(types.EventTypeSecretRevealed, "reveal window closed, secret fully revealed")
}

func (sm *SecretStateMachine) afterSecretCancelled(e *fsm.Event) {
	sm.emitStateTransitionEvent(types.EventTypeSecretCancelled, "secret cancelled by creator")
}

func (sm *SecretStateMachine) afterDistributionTimeout(e *fsm.Event) {
	sm.emitStateTransitionEvent(types.EventTypeSecretFailed, "distribution timeout - creator failed to distribute shares")
}

func (sm *SecretStateMachine) afterAcceptanceTimeout(e *fsm.Event) {
	sm.emitStateTransitionEvent(types.EventTypeSecretFailed, "acceptance timeout - insufficient guardian acceptances")
}

func (sm *SecretStateMachine) afterRevealTimeout(e *fsm.Event) {
	sm.emitStateTransitionEvent(types.EventTypeSecretFailed, "reveal timeout - insufficient shares revealed")
}

func (sm *SecretStateMachine) emitStateTransitionEvent(eventType, description string) {
	sdkCtx := sdk.UnwrapSDKContext(sm.ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			eventType,
			sdk.NewAttribute(types.AttributeKeySecretId, sm.secretId),
			sdk.NewAttribute("new_state", sm.fsm.Current()),
			sdk.NewAttribute("description", description),
			sdk.NewAttribute("block_height", fmt.Sprintf("%d", sdkCtx.BlockHeight())),
		),
	)
}

// TransitionSecretState validates and applies a state transition on the
// caller's in-memory secret, then persists it. The FSM's CanTransition guard is
// the sole gate — there is deliberately no read-back of the stored record, so
// the caller's pending field changes land in the same single write. Callers
// must therefore pass the secret they have been mutating (never a stale copy)
// and must not SetSecret again afterwards.
func (k Keeper) TransitionSecretState(ctx context.Context, secret *types.Secret, event string) error {
	sm := NewSecretStateMachine(secret.Id, secret.State, k, ctx)

	if !sm.CanTransition(event) {
		return fmt.Errorf("invalid transition: cannot perform %s from state %s", event, sm.CurrentState())
	}

	if err := sm.TransitionTo(event); err != nil {
		return err
	}

	secret.State = sm.CurrentState()

	// Stamp the terminal height once, on the transition that makes the secret
	// terminal (revealed/cancelled/failed) — this is what retention keys on
	becameTerminal := secret.IsComplete() && secret.TerminalAt == 0
	if becameTerminal {
		secret.TerminalAt = sdk.UnwrapSDKContext(ctx).BlockHeight()
	}

	if err := k.SetSecret(ctx, *secret); err != nil {
		return err
	}

	// Stage 1 retention fires exactly once, after the terminal record is
	// persisted: every store it deletes (shares, assignments, slash marks)
	// has already been read by whichever path drove this transition —
	// settlement partitions, cancellation payouts and commit-timeout bond
	// releases all complete their reads before transitioning. It also
	// schedules the Stage 2 prune. Idempotent, so an error here (retried by
	// the settlement queue) cannot double-fire.
	if becameTerminal {
		return k.onSecretTerminal(ctx, *secret)
	}
	return nil
}
