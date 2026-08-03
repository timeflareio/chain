package keeper_test

import (
	"crypto/sha256"
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/curve25519"

	"github.com/timeflareio/chain/x/secrets/types"
	"github.com/timeflareio/crypto/go"
)

// shareKey builds the (secret_id, guardian_address) key the side-stores use.
func shareKey(secretID, guardian string) collections.Pair[string, string] {
	return collections.Join(secretID, guardian)
}

// testShareBytes derives a deterministic, sub-cap share stand-in for a
// guardian (32B — key-share-envelope scale; the chain treats share bytes as
// opaque, so tests only need determinism and valid sizing).
func testShareBytes(secretID, guardian string) []byte {
	sum := sha256.Sum256([]byte("share:" + secretID + ":" + guardian))
	return sum[:]
}

// seedSealFields backfills what UserDistributeShares would have stored for a
// hand-built (already-distributed) secret fixture: the payload ciphertext in
// the payload store and the per-secret public key on the slim record.
func seedSealFields(t *testing.T, f *fixture, secretID string) {
	t.Helper()
	require.NoError(t, f.keeper.SecretPayloads.Set(f.ctx, secretID, testPayloadCiphertext()))
	secret, err := f.keeper.GetSecret(f.ctx, secretID)
	require.NoError(t, err)
	secret.SecretPublicKey = testSecretPublicKey()
	require.NoError(t, f.keeper.SetSecret(f.ctx, secret))
}

// testPayloadCiphertext and testSecretPublicKey provide valid seal fields for
// tests that drive UserDistributeShares without caring about the cryptography —
// the chain stores both opaquely (length-validated only).
func testPayloadCiphertext() []byte {
	payload := make([]byte, 200)
	for i := range payload {
		payload[i] = byte(0xC1 ^ i)
	}
	return payload
}

func testSecretPublicKey() []byte {
	key := make([]byte, types.SecretPublicKeySize)
	for i := range key {
		key[i] = byte(0x5C ^ i)
	}
	return key
}

// Test support for the split secret model: secrets store only slim metadata,
// while per-guardian data lives in side-stores keyed (secret_id, guardian).
// These helpers seed those side-stores the way UserDistributeShares/GuardianConfirmShares/
// GuardianRevealShare would, so tests can build any lifecycle state directly.

// writeShareRecord stores a guardian's encrypted share + HMAC (the cold record
// UserDistributeShares writes).
func writeShareRecord(t *testing.T, f *fixture, secretID, guardian string, encryptedShare, shareHmac []byte) {
	t.Helper()
	err := f.keeper.SecretShares.Set(f.ctx, shareKey(secretID, guardian), types.SecretShareData{
		EncryptedShare: encryptedShare,
		ShareHmac:      shareHmac,
	})
	require.NoError(t, err)
}

// writeAssignmentRecord stores a guardian's assignment status (the hot record
// UserDistributeShares creates as PROPOSED and GuardianConfirmShares flips).
func writeAssignmentRecord(t *testing.T, f *fixture, secretID, guardian string, status types.AssignmentStatus, respondedAt int64) {
	t.Helper()
	err := f.keeper.SetAssignment(f.ctx, secretID, guardian, types.AssignmentRecord{
		Status:           status,
		RespondedAtBlock: respondedAt,
	})
	require.NoError(t, err)
}

// writeReveal stores a revealed share (what GuardianRevealShare records).
func writeReveal(t *testing.T, f *fixture, secretID, guardian string, decryptedShare []byte, revealedAt int64) {
	t.Helper()
	err := f.keeper.SecretReveals.Set(f.ctx, shareKey(secretID, guardian), types.RevealedShare{
		GuardianAddress: guardian,
		DecryptedShare:  decryptedShare,
		RevealedAtBlock: revealedAt,
	})
	require.NoError(t, err)
}

// assignmentStatus fetches a guardian's assignment status, failing the test if
// no record exists.
func assignmentStatus(t *testing.T, f *fixture, secretID, guardian string) types.AssignmentStatus {
	t.Helper()
	record, err := f.keeper.GetAssignment(f.ctx, secretID, guardian)
	require.NoError(t, err, "no assignment record for guardian %s on secret %s", guardian, secretID)
	return record.Status
}

// acceptedGuardians returns the accepted (= active) guardian set for a secret.
func acceptedGuardians(t *testing.T, f *fixture, secretID string) []string {
	t.Helper()
	accepted, err := f.keeper.AcceptedGuardians(f.ctx, secretID)
	require.NoError(t, err)
	return accepted
}

// revealsFor returns a secret's revealed shares in guardian-address order.
func revealsFor(t *testing.T, f *fixture, secretID string) []types.RevealedShare {
	t.Helper()
	reveals, err := f.keeper.RevealsForSecret(f.ctx, secretID)
	require.NoError(t, err)
	return reveals
}

// testDetectionHint returns a shape-valid recipient discovery hint. The chain
// validates only shape (version, 32B ephemeral key, 8B tag) — content is
// deliberately unverifiable, so static bytes are exactly as valid as a real
// derivation (the random-hint opt-out relies on this).
// testDetectionHint is a REAL hint: its tag is derived from an actual X25519
// exchange between the fixture recipient key and the fixture ephemeral key, so
// recipiencyProofFor can produce a proof the chain accepts. A placeholder tag
// would make every rebate collection untestable.
func testDetectionHint() types.DetectionHint {
	ephemeralPub, shared := testHintExchange(nil)
	return types.DetectionHint{
		Version:      types.DetectionHintVersion,
		EphemeralPub: ephemeralPub,
		Tag:          crypto.DetectionTag(shared),
	}
}

// testHintExchange performs the fixture's ephemeral-static X25519 exchange,
// returning the ephemeral public key R and the shared value both sides derive.
// t may be nil (testDetectionHint has no testing handle); the keys are fixed,
// so an error here is a broken build, not a test failure.
func testHintExchange(t *testing.T) (ephemeralPub, shared []byte) {
	if t != nil {
		t.Helper()
	}
	var recipientPriv, ephemeralPriv [32]byte
	copy(recipientPriv[:], []byte("timeflare-fixture-recipient-key1"))
	copy(ephemeralPriv[:], []byte("timeflare-fixture-ephemeral-key1"))

	recipientPub, err := curve25519.X25519(recipientPriv[:], curve25519.Basepoint)
	if err != nil {
		panic(err)
	}
	ephemeralPub, err = curve25519.X25519(ephemeralPriv[:], curve25519.Basepoint)
	if err != nil {
		panic(err)
	}
	shared, err = curve25519.X25519(ephemeralPriv[:], recipientPub)
	if err != nil {
		panic(err)
	}
	return ephemeralPub, shared
}

// recipiencyProofFor returns the proof the fixture recipient would submit to
// collect a secret's rebate: z = X25519(a, R), which the chain hashes against
// the stored hint tag.
func recipiencyProofFor(t *testing.T, secret types.Secret) []byte {
	t.Helper()
	var recipientPriv [32]byte
	copy(recipientPriv[:], []byte("timeflare-fixture-recipient-key1"))
	proof, err := curve25519.X25519(recipientPriv[:], secret.DetectionHint.EphemeralPub)
	require.NoError(t, err)
	return proof
}
