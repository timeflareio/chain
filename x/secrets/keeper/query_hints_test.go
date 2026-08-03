package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/timeflareio/chain/x/secrets/keeper"
	"github.com/timeflareio/chain/x/secrets/types"
)

// seedHintSecret writes a minimal secret + its creation-feed entry directly,
// bypassing the msg flow — HintsSince only reads the feed.
func seedHintSecret(t *testing.T, f *fixture, id string, createdAt int64) types.DetectionHint {
	t.Helper()
	hint := testDetectionHint()
	hint.Tag = []byte(fmt.Sprintf("%08d", createdAt))[:8] // distinct per secret
	secret := types.Secret{
		Id:            id,
		Creator:       "tmflr1j4u6tpuqjgjccq42srkpks5yhcfr6p48yfertz",
		CreatedAt:     createdAt,
		DetectionHint: hint,
		State:         types.SECRET_STATUS_RESERVED,
	}
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
	require.NoError(t, f.keeper.IndexSecretCreation(f.ctx, secret))
	return hint
}

func TestHintsSince_CursorSemantics(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Three secrets across three heights, two sharing a height
	seedHintSecret(t, f, types.GenerateValidSecretID(), 100)
	h200a := seedHintSecret(t, f, types.GenerateValidSecretID(), 200)
	h200b := seedHintSecret(t, f, types.GenerateValidSecretID(), 200)
	h300 := seedHintSecret(t, f, types.GenerateValidSecretID(), 300)

	// Full scan from genesis
	resp, err := qs.HintsSince(f.ctx, &types.QueryHintsSinceRequest{SinceHeight: 0})
	require.NoError(t, err)
	require.Len(t, resp.Hints, 4)
	// Creation order: heights ascend
	require.Equal(t, int64(100), resp.Hints[0].CreatedAt)
	require.Equal(t, int64(300), resp.Hints[3].CreatedAt)

	// Resume from height 200 (inclusive): skips the height-100 entry
	resp, err = qs.HintsSince(f.ctx, &types.QueryHintsSinceRequest{SinceHeight: 200})
	require.NoError(t, err)
	require.Len(t, resp.Hints, 3)
	tags := map[string]bool{}
	for _, h := range resp.Hints {
		tags[string(h.DetectionHint.Tag)] = true
	}
	require.True(t, tags[string(h200a.Tag)] && tags[string(h200b.Tag)] && tags[string(h300.Tag)])

	// Resume past everything: empty, no next key
	resp, err = qs.HintsSince(f.ctx, &types.QueryHintsSinceRequest{SinceHeight: 301})
	require.NoError(t, err)
	require.Empty(t, resp.Hints)
	require.Empty(t, resp.Pagination.NextKey)

	// Truncation: limit 2 → next key carries the resume height
	resp, err = qs.HintsSince(f.ctx, &types.QueryHintsSinceRequest{
		SinceHeight: 0,
		Pagination:  &query.PageRequest{Limit: 2},
	})
	require.NoError(t, err)
	require.Len(t, resp.Hints, 2)
	require.NotEmpty(t, resp.Pagination.NextKey)

	// Negative height rejected
	_, err = qs.HintsSince(f.ctx, &types.QueryHintsSinceRequest{SinceHeight: -1})
	require.Error(t, err)
}
