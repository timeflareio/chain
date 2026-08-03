package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/timeflareio/chain/x/secrets/types"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	k Keeper
}

// NewQueryServerImpl returns an implementation of the secrets QueryServer interface
func NewQueryServerImpl(k Keeper) types.QueryServer {
	return &queryServer{k}
}

// assembleSecretView joins a slim secret record with its side-stores into the
// full pre-split wire shape. Query-side only — consensus paths never assemble.
func (qs queryServer) assembleSecretView(ctx context.Context, secret types.Secret) (*types.SecretView, error) {
	// The acceptance tally lives in its own record (it is the only field an
	// acceptance mutates, and rewriting the whole secret to bump it made a
	// guardian's gas scale with the band). Join it HERE rather than at each
	// call site: the paginated queries hand this function a value straight
	// from the collection, which never carries the tally, so a caller-side
	// join is one that eventually gets forgotten — as it was.
	secret, err := qs.k.withAcceptedCount(ctx, secret)
	if err != nil {
		return nil, err
	}

	view := &types.SecretView{
		Id:                  secret.Id,
		Creator:             secret.Creator,
		DetectionHint:       secret.DetectionHint,
		RevealStartBlock:    secret.RevealStartBlock,
		RevealEndBlock:      secret.RevealEndBlock,
		Threshold:           secret.Threshold,
		MinShares:           secret.MinShares,
		MaxShares:           secret.MaxShares,
		SecretCommitment:    secret.SecretCommitment,
		CommitDeadline:      secret.CommitDeadline,
		State:               secret.State,
		RewardPool:          secret.RewardPool,
		CreatedAt:           secret.CreatedAt,
		Bump:                secret.Bump,
		AcceptFees:          secret.AcceptFees,
		GuardianBondAmounts: secret.GuardianBondAmounts,
		SelectedGuardians:   secret.SelectedGuardians,
		AcceptedCount:       secret.AcceptedCount,
		RevealedCount:       secret.RevealedCount,
		TerminalAt:          secret.TerminalAt,
		SecretPublicKey:     secret.SecretPublicKey,
		RebateAmount:        secret.RebateAmount,
		RebateCollected:     secret.RebateCollected,
	}

	// Assignments are presented in Phase-1 selection order, exactly as the
	// pre-split model stored them: every selected guardian appears from
	// request time (PROPOSED, no share bytes yet); share data and response
	// status are joined in from the side-stores once distribution/confirmation
	// have written them. Clients therefore see the selected set immediately
	// after UserRequestGuardians, as they always did.
	for _, guardianAddress := range secret.SelectedGuardians {
		assignment := &types.GuardianAssignment{
			GuardianAddress: guardianAddress,
			Status:          types.AssignmentStatus_ASSIGNMENT_STATUS_PROPOSED,
		}
		if record, err := qs.k.GetAssignment(ctx, secret.Id, guardianAddress); err == nil {
			assignment.Status = record.Status
			assignment.RespondedAtBlock = record.RespondedAtBlock
		}
		if share, err := qs.k.GetShareData(ctx, secret.Id, guardianAddress); err == nil {
			assignment.EncryptedShare = share.EncryptedShare
			assignment.ShareHmac = share.ShareHmac
		}
		view.GuardianAssignments = append(view.GuardianAssignments, assignment)
		if assignment.Status == types.AssignmentStatus_ASSIGNMENT_STATUS_ACCEPTED {
			view.ActiveAssignments = append(view.ActiveAssignments, guardianAddress)
		}
	}

	reveals, err := qs.k.RevealsForSecret(ctx, secret.Id)
	if err != nil {
		return nil, err
	}
	for i := range reveals {
		view.RevealedShares = append(view.RevealedShares, &reveals[i])
	}

	return view, nil
}

func (qs queryServer) Secret(ctx context.Context, req *types.QuerySecretRequest) (*types.QuerySecretResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret ID cannot be empty")
	}

	secret, err := qs.k.GetSecret(ctx, req.SecretId)
	if err != nil {
		if err == collections.ErrNotFound {
			return nil, status.Error(codes.NotFound, "secret not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	view, err := qs.assembleSecretView(ctx, secret)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QuerySecretResponse{Secret: view}, nil
}

func (qs queryServer) Secrets(ctx context.Context, req *types.QuerySecretsRequest) (*types.QuerySecretsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	secrets, pageRes, err := query.CollectionPaginate(
		ctx,
		qs.k.Secrets,
		req.Pagination,
		func(key string, value types.Secret) (*types.SecretView, error) {
			return qs.assembleSecretView(ctx, value)
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QuerySecretsResponse{Secrets: secrets, Pagination: pageRes}, nil
}

func (qs queryServer) SecretsByCreator(ctx context.Context, req *types.QuerySecretsByCreatorRequest) (*types.QuerySecretsByCreatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Creator == "" {
		return nil, status.Error(codes.InvalidArgument, "creator cannot be empty")
	}

	// Paginate over the creator index; each entry is a point-read + assemble
	secrets, pageRes, err := query.CollectionPaginate(
		ctx,
		qs.k.SecretsByCreator,
		req.Pagination,
		func(key collections.Pair[string, string], _ collections.NoValue) (*types.SecretView, error) {
			secret, err := qs.k.GetSecret(ctx, key.K2())
			if err != nil {
				return nil, err
			}
			return qs.assembleSecretView(ctx, secret)
		},
		query.WithCollectionPaginationPairPrefix[string, string](req.Creator),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QuerySecretsByCreatorResponse{Secrets: secrets, Pagination: pageRes}, nil
}

func (qs queryServer) PendingSecrets(ctx context.Context, req *types.QueryPendingSecretsRequest) (*types.QueryPendingSecretsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// A secret is "pending" while its reveal phase is active: PENDING
	// (activated, threshold not yet met) or RECONSTRUCTABLE (threshold met,
	// window still open) — the same definition the settlement queue's state
	// guard uses for an active reveal phase
	secrets, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		qs.k.Secrets,
		req.Pagination,
		func(key string, value types.Secret) (bool, error) {
			return value.State == types.SECRET_STATUS_PENDING || value.State == types.SECRET_STATUS_RECONSTRUCTABLE, nil
		},
		func(key string, value types.Secret) (*types.SecretView, error) {
			return qs.assembleSecretView(ctx, value)
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryPendingSecretsResponse{Secrets: secrets, Pagination: pageRes}, nil
}

// SecretMeta returns only the slim metadata record — the cheap call for light clients.
func (qs queryServer) SecretMeta(ctx context.Context, req *types.QuerySecretMetaRequest) (*types.QuerySecretMetaResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret ID cannot be empty")
	}

	secret, err := qs.k.GetSecret(ctx, req.SecretId)
	if err != nil {
		if err == collections.ErrNotFound {
			return nil, status.Error(codes.NotFound, "secret not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QuerySecretMetaResponse{Secret: &secret}, nil
}

// SecretAssignments returns the per-guardian status records for a secret (no share bytes).
func (qs queryServer) SecretAssignments(ctx context.Context, req *types.QuerySecretAssignmentsRequest) (*types.QuerySecretAssignmentsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret ID cannot be empty")
	}

	resp := &types.QuerySecretAssignmentsResponse{}
	err := qs.k.WalkAssignments(ctx, req.SecretId, func(guardianAddress string, record types.AssignmentRecord) (bool, error) {
		hasShare, err := qs.k.SecretShares.Has(ctx, collections.Join(req.SecretId, guardianAddress))
		if err != nil {
			return true, err
		}
		resp.Assignments = append(resp.Assignments, types.SecretAssignmentView{
			GuardianAddress:  guardianAddress,
			Status:           record.Status,
			RespondedAtBlock: record.RespondedAtBlock,
			HasShare:         hasShare,
		})
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return resp, nil
}

// SecretReveals returns a secret's revealed shares — what a recipient needs to reconstruct.
func (qs queryServer) SecretReveals(ctx context.Context, req *types.QuerySecretRevealsRequest) (*types.QuerySecretRevealsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret ID cannot be empty")
	}

	reveals, err := qs.k.RevealsForSecret(ctx, req.SecretId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	resp := &types.QuerySecretRevealsResponse{}
	for i := range reveals {
		resp.Reveals = append(resp.Reveals, &reveals[i])
	}
	return resp, nil
}

// SecretShare returns a single guardian's encrypted share for a secret — what
// a guardian daemon needs, without the other guardians' shares.
func (qs queryServer) SecretShare(ctx context.Context, req *types.QuerySecretShareRequest) (*types.QuerySecretShareResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret ID cannot be empty")
	}
	if req.GuardianAddress == "" {
		return nil, status.Error(codes.InvalidArgument, "guardian address cannot be empty")
	}

	share, err := qs.k.SecretShares.Get(ctx, collections.Join(req.SecretId, req.GuardianAddress))
	if err != nil {
		if err == collections.ErrNotFound {
			return nil, status.Error(codes.NotFound, "share not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QuerySecretShareResponse{Share: &share}, nil
}

// SecretPayload returns a secret's stored payload ciphertext (C) — what a
// reconstruction client needs alongside the revealed key shares.
func (qs queryServer) SecretPayload(ctx context.Context, req *types.QuerySecretPayloadRequest) (*types.QuerySecretPayloadResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret ID cannot be empty")
	}

	payload, err := qs.k.SecretPayloads.Get(ctx, req.SecretId)
	if err != nil {
		if err == collections.ErrNotFound {
			return nil, status.Error(codes.NotFound, "payload not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QuerySecretPayloadResponse{PayloadCiphertext: payload}, nil
}

// SecretTombstone returns the permanent tombstone of a pruned secret,
// distinguishing "pruned" (archived, digest-verifiable) from "never existed"
// (NotFound on both this and Query/Secret).
func (qs queryServer) SecretTombstone(ctx context.Context, req *types.QuerySecretTombstoneRequest) (*types.QuerySecretTombstoneResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret ID cannot be empty")
	}

	tombstone, err := qs.k.SecretTombstones.Get(ctx, req.SecretId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "tombstone not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QuerySecretTombstoneResponse{Tombstone: tombstone}, nil
}

// Guardian returns guardian information for a given address
func (qs queryServer) Guardian(ctx context.Context, request *types.QueryGuardianRequest) (*types.QueryGuardianResponse, error) {
	if request == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty request")
	}

	if request.Address == "" {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "address cannot be empty")
	}

	guardian, found := qs.k.GetGuardian(ctx, request.Address)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrGuardianNotFound, "guardian %s not found", request.Address)
	}

	return &types.QueryGuardianResponse{Guardian: guardian}, nil
}

// Guardians returns all guardians with pagination
func (qs queryServer) Guardians(ctx context.Context, request *types.QueryGuardiansRequest) (*types.QueryGuardiansResponse, error) {
	if request == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty request")
	}

	guardians, pageRes, err := query.CollectionPaginate(
		ctx,
		qs.k.Guardians,
		request.Pagination,
		func(key string, value types.Guardian) (types.Guardian, error) {
			return value, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return &types.QueryGuardiansResponse{
		Guardians:  guardians,
		Pagination: pageRes,
	}, nil
}

// GuardianKeyHistory returns a guardian's append-only key-epoch history in
// epoch order — what a daemon or client needs to resolve the epoch in force
// at any height (the newest entry with effective_from_height <= h).
// Histories are small by construction (the rotation interval bounds growth
// to ~12 entries per guardian-year), so the response is unpaginated.
func (qs queryServer) GuardianKeyHistory(ctx context.Context, request *types.QueryGuardianKeyHistoryRequest) (*types.QueryGuardianKeyHistoryResponse, error) {
	if request == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty request")
	}

	if request.Address == "" {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "address cannot be empty")
	}

	if _, found := qs.k.GetGuardian(ctx, request.Address); !found {
		return nil, errorsmod.Wrapf(types.ErrGuardianNotFound, "guardian %s not found", request.Address)
	}

	var epochs []types.GuardianKeyEpoch
	err := qs.k.WalkGuardianKeyHistory(ctx, request.Address, func(epoch uint64, entry types.KeyHistoryEntry) (bool, error) {
		epochs = append(epochs, types.GuardianKeyEpoch{Epoch: epoch, Entry: entry})
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryGuardianKeyHistoryResponse{Epochs: epochs}, nil
}

// hintsPageLimit bounds one HintsSince response. Records are ~50B, so the cap
// keeps responses comfortably small while letting a cold scan catch up fast.
const hintsPageLimit = 1000

// HintsSince serves the incremental discovery-scan feed: hint records for
// secrets created at or after since_height, in (created_at, secret_id) order.
// Duplicates across resumes are harmless (hint testing is idempotent), so the
// resume cursor is simply the last created_at seen; when a response is
// truncated, PageResponse.NextKey carries the big-endian created_at to resume
// from.
func (qs queryServer) HintsSince(ctx context.Context, req *types.QueryHintsSinceRequest) (*types.QueryHintsSinceResponse, error) {
	if req == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty request")
	}
	if req.SinceHeight < 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "since_height cannot be negative")
	}

	limit := uint64(hintsPageLimit)
	if req.Pagination != nil && req.Pagination.Limit > 0 && req.Pagination.Limit < hintsPageLimit {
		limit = req.Pagination.Limit
	}

	rng := new(collections.Range[collections.Pair[int64, string]]).
		StartInclusive(collections.Join(req.SinceHeight, ""))

	hints := make([]types.HintRecord, 0, limit)
	var nextHeight int64
	truncated := false
	err := qs.k.HintsByCreation.Walk(ctx, rng, func(key collections.Pair[int64, string], hint types.DetectionHint) (bool, error) {
		if uint64(len(hints)) >= limit {
			truncated = true
			nextHeight = key.K1()
			return true, nil
		}
		hints = append(hints, types.HintRecord{
			SecretId:      key.K2(),
			CreatedAt:     key.K1(),
			DetectionHint: hint,
		})
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	pageRes := &query.PageResponse{}
	if truncated {
		pageRes.NextKey = sdk.Uint64ToBigEndian(uint64(nextHeight))
	}

	return &types.QueryHintsSinceResponse{Hints: hints, Pagination: pageRes}, nil
}
