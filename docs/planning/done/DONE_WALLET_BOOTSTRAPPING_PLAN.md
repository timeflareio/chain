# Wallet Bootstrapping — Plan

*A Cosmos address that has never received anything cannot sign. Two protocol
payments are pulled by their beneficiary, so both are unreachable by exactly the
people they exist to reach. This plan gets a new wallet its first VEIL.*

> **Status: done — 31 July 2026**, branch `worktree-wallet-bootstrapping`, PR #125.
> Route A only; every open question ruled. Proven on the native devnet: a
> never-funded address swept a funded courier and then signed a transaction,
> which it could not do before (S11).
>
> **Priority**: P1 — the recipient rebate is the protocol's distribution
> mechanism and its intended beneficiaries cannot collect.
>
> **Origin**: found while implementing
> [DONE_REBATE_COLLECTION_UX_PLAN.md](DONE_REBATE_COLLECTION_UX_PLAN.md)
> (§5), verified on the native devnet 30 July 2026. Owner ruling 31 July 2026:
> **the recipient stays the rebate's beneficiary** — distribution to recipients
> is how the user pool decentralises over time, and moving the rebate to the
> sender would trade that away. Only the getting-started funds problem is in
> scope.
>
> **Components**: `typescript-sdk/` (claim URI format, courier sweep),
> `mobile-client/app/` (creation, import, copy), `docs/guides/CLIENT_CONVENTIONS.md`
> and `testdata/vectors/client_conventions.json` (the URI format is a pinned
> ecosystem convention, so the format change lands with its documentation and its
> vectors), `devnet/` e2e scenarios. No chain, proto or `docs/spec.md` change —
> route A is entirely client-side, which is why it is the whole of this plan.
>
> **Bounded by**: the rebate collection UX plan, whose merged funding prompt
> (§2.8) is route C here. §4 hands the general case and the early-reveal bounty
> to a plan not yet written.

## 1. The defect

Cosmos writes an `auth` account record — account number and sequence — only when
an address **first receives funds**. Without it there is nothing to sign
against, so no transaction can be built at all. Verified with a freshly
generated key against the running devnet:

```
account tmflr14yvy7mldnhsn7vyxkdzvefzsx6yepzsv0mk0m3 not found: key not found
```

This is not a fee problem. A fee waiver would not help: the ante handler still
needs the account to exist for the sequence and public key. Nor would the
`feegrant` module, for the same reason. **Value can only reach a new address by
someone else sending it.**

Two protocol payments are structured as pulls, and so inherit this:

### 1.1 The recipient rebate

`MsgRecipientCommitRebate` and `MsgRecipientCollectRebate` are both signed by
the collecting address. Collecting costs 23,000 uveil in fees — 9,000 for the
commit, 14,000 for the reveal — paid *before* the rebate arrives.

The mechanism exists to seed new wallets, so its intended beneficiary is
precisely the address that cannot transact. The mobile Funding screen states the
promise plainly ("it arrives one of two ways — a rebate on a secret someone sent
you"), and today that promise cannot be kept.

### 1.2 The early-reveal bounty

Worse, and previously unnoticed. `MsgSlashGuardian` carries a `reporter_address`
that receives **50% of the slashed bond** (`EarlyRevealReporterPercent`), and
`cosmos.msg.v1.signer` is set to that same field — so the reporter must sign.

Three things make this sharper than the rebate:

- **The bounty is large** — half a bond, against a rebate that need only clear
  0.05 VEIL.
- **The best-placed reporter may be the least funded.** The recipient can detect
  an early reveal directly, by finding the secret reconstructable before its
  window opens. Others may notice (a guardian, a watcher, the creator) and those
  hold funded wallets, so this is a judgement about who is *best* placed rather
  than a claim that only recipients report — but the recipient is both the
  harmed party and the most likely unfunded one.
- **It cannot be waited out.** Reports are refused once the reveal window opens
  (`msg_server_slash_guardian.go:64`). A rebate waits three months; a report may
  have minutes. "Go and get funded first" is not available.

There is a systemic cost beyond the individual: the bounty exists to make
early-reveal detection self-policing. If the parties best placed to report are
structurally unable to, the deterrent is weaker than the spec claims.

## 2. What is NOT the answer

Recorded because each looks plausible and costs real work to discover:

- **A fee waiver, or `feegrant`.** Removes the cost, not the blocker — the
  signer's account must still exist.
- **Paying the rebate automatically at settlement.** The chain does not know the
  recipient's address; that is the entire reason collection is a pull with a
  recipiency proof. Putting the address on the secret would deanonymise the
  recipient, which the detection-hint design exists to prevent.
- **Moving the rebate to the sender.** Dissolves the problem by removing the
  unfunded beneficiary, and would delete a great deal of machinery (no proof, no
  commit–reveal, no expiry). Ruled out 31 July 2026: it pays the distribution
  pool back to people who already hold VEIL, concentrating rather than spreading,
  and no new wallet ever gets its first VEIL. It would also not fix §1.2, so the
  problem would still need solving.
- **Letting the ante handler create an account for a fee-free first
  transaction.** The only route needing no third party, and the most dangerous:
  it opens free transactions from addresses that do not exist, which is an
  unbounded spam surface. Not pursued.

## 3. Route A — a funded claim kit (client only, no protocol change)

The claim kit is **already a bearer envelope containing a private key**:
`encodeClaimUri(privateKey, secretId)` carries the recipient's X25519 identity
key. Whoever holds the kit can already decrypt the secret. So the kit is the
natural place to also carry the means to transact.

### 3.0a The sender is told what the seed is, and may add to it

The seed is a charge on the sender, so it is itemised in the creation cost
breakdown rather than deducted quietly — alongside the pool, the gas cover and
the creation fee, and counted in the total the sufficiency check uses. A charge
that does not appear in the total is a charge that fails on chain.

It reads differently from every other line, because it is: the pool and the gas
cover are refundable, the creation fee is spent, and this **goes to the
recipient**. The copy says what it buys — a wallet cannot send anything until it
has received something once, so without this the recipient cannot collect the
rebate the protocol will credit them.

**Mandatory at the floor, raisable above it.** The 0.2 VEIL minimum is not
optional for a claim kit (§3), but a sender who wants to give the recipient more
than activation money may raise it, and the field says so. That reuses the
wizard's existing stepper, which already supports tap-to-type for values coarse
stepping cannot reach — the same pattern the gas price uses, since a step is a UI
convenience and never a legality bound.

Lowering below the floor is refused: under it the recipient cannot complete a
collection, which is the entire point of the seed.

**A funded kit is the default, not an option.** Reaching for a claim kit *is* the
statement that the recipient is new: a sender with a known contact addresses the
secret to that contact's existing identity instead. So kit mode implies an
unfunded recipient, and seeding it is the default rather than a checkbox nobody
finds. Client-side enforcement only — nothing on chain compels it — but the right
default costs the sender 0.22 VEIL and removes the failure entirely.

The seed is **0.2 VEIL**, derived from the gas constants rather than written down
(as the funding ask in the rebate UX plan §2.8 already is). It covers the sweep's
own fee plus a full rebate collection with headroom to spare.

Where the sender's balance cannot cover the pool, the fees *and* the seed, the
kit is still created — unfunded, with the shortfall stated. Blocking a secret
outright over an onboarding courtesy would be the wrong trade.

**The shape.** At creation:

1. The app generates a throwaway secp256k1 keypair `T`.
2. The sender sends the seed amount to `T`. **`T`'s account now exists** — it
   received funds.
3. The kit carries `T`'s private key alongside the identity key.
4. On import, the recipient's app **signs from `T`** (which can sign, because it
   exists) and sweeps the balance to the recipient's own wallet address `R`.
   That transfer **creates `R`'s account**.
5. `R` can now collect rebates, report early reveals, and seal its own secrets.

### 3.0 The kit format, and how an old client refuses a seeded one

The claim URI is `timeflare:claim/<bech32m tfsk…>?id=<uuid>`, and its payload
already carries a version byte with the rule this needs
(`CLIENT_CONVENTIONS.md` §4): *parsers encountering an unknown version reject
with an "update your client" error — they never guess.*

So a seeded kit is **version 2**, and the courier key rides as its own bech32m
payload in a query parameter under a distinct HRP:

```
timeflare:claim/<bech32m tfsk…v2>?id=<uuid>&seed=<bech32m tfck…>
```

Three properties fall out, each deliberate:

- **An old client refuses it outright.** `decodePayload` throws on an unknown
  version, so a pre-change app cannot import the identity while silently
  abandoning the seed in `T`. Refusing is the correct failure: the funds stay
  recoverable by re-importing on an updated app, where dropping them would strand
  them forever.
- **An unseeded kit stays version 1**, so nothing regresses for the existing
  flow and older apps keep working exactly as now.
- **A distinct HRP (`tfck`, courier key) makes the two keys unconfusable.** An
  identity key is `tfsk`; a courier is `tfck`. Given §3.1's warning about
  conflating key roles, the encoding should make the mistake impossible rather
  than merely documented.

A query parameter rather than a longer payload because bech32m's error-detection
guarantees hold only to 90 characters, and two 32-byte keys in one payload
exceeds that. Two payloads of 33 bytes each keep the guarantee.

### 3.1 Four keys, four jobs — `T` is not any of the protocol's keys

Written out because the kit format invites exactly one dangerous conflation:
that the courier key could be the per-secret key the kit already implies. It
cannot.

| Key | Curve | Who holds it | Purpose |
|---|---|---|---|
| Recipient identity | X25519 | the recipient, via the kit | decrypts `C_r`; proves recipiency for the rebate |
| Per-secret (outer) | X25519 | **nobody** — split `t`-of-`n` across guardians | the time-lock |
| Recipient wallet | secp256k1 | the recipient, from onboarding | signs; receives the sweep |
| **Courier `T`** | **secp256k1** | **the kit, and nowhere else** | holds the seed, makes one transfer, is abandoned |

**The per-secret key must never be the courier.** Its entire purpose is to be
reconstructable by a quorum — that *is* the time-lock (`rust/src/seal.rs:135-146`
splits its raw 32-byte scalar). Funding that address would mean:

- any `threshold` guardians could reassemble the scalar and sweep it;
- worse, once the reveal window closes the shares are public **by design**, so
  anyone at all could — the protocol deliberately publishes that private key;
- the timing is backwards regardless: it only becomes reconstructable *after* the
  reveal window, long after the recipient needs funds to collect a rebate, and
  the recipient cannot derive it earlier without breaking the lock;
- and the curves do not match — protocol keys are X25519, wallets secp256k1, and
  reusing one scalar across two algorithms is unsound even where the bytes fit.

So `T` is generated fresh for this purpose alone, is never split, never given to
a guardian, never written to chain, and never adopted as a wallet.

What makes the scheme work is not a property of any key but the order of
existence: **the sender funding `T` is what brings `T`'s account into being**, so
`T` can sign the one transfer that brings the recipient's account into being.
Sender has funds → `T` exists → `R` exists.

**Why sweep rather than adopt.** `T` is never adopted as the recipient's wallet.
The app keeps the wallet and identity key domains strictly separate — an explicit
rule, since blurring them is a UX trap — and a key the *sender* generated must
never become the recipient's long-term wallet, because the sender knows it. `T`
is a courier: it exists to make one transfer and then be empty.

**What it costs the sender**: the seed plus two send fees. At a 0.2 VEIL seed
that is roughly 0.22 VEIL — negligible, and optional.

**Why this is the strongest first move**: no protocol change, no new component,
no third party, and it covers the dominant onboarding path — a sender bringing
someone new onto the network. It also incidentally fixes §1.2 for those
recipients, since a funded `R` can report.

**Residual**: the sender must opt in, and it does nothing for a recipient who
already holds an identity key and is sent a secret without a kit. An unclaimed
kit leaves the seed sitting in `T` indefinitely.

**No reclaim affordance is built** (ruled 31 July 2026). The sender's app could
retain `T`'s key and sweep it back, but that would race a recipient mid-import —
turning a gift into a rug-pull, by accident or otherwise — and would keep one more
spendable key on the sender's device. Against 0.2 VEIL neither is worth it.
Instead the creation screen states plainly that the seed travels with the kit and
does not come back if the kit is never used. Revisit only if senders ask for it.

### 3.1a A funded kit will not import without somewhere to sweep to

The sweep needs a destination, so importing a seeded kit is **refused** unless
this device has a wallet address — and the address is validated (bech32, chain
prefix) before anything is persisted, not assumed well-formed because it came
from local storage.

Refusing beats importing-and-deferring. A half-imported kit would leave the
identity on the device and the seed in a courier nobody is tracking, which is the
stranded-funds outcome in a different costume. Refused, the kit remains a
complete, re-importable artefact.

In practice this is defensive: onboarding creates a wallet before any import
screen is reachable. It is validated anyway because the cost of being wrong is
someone's funds, and because a future entry point (a deep link, a restore flow)
could reach import earlier than onboarding does.

Nothing is persisted before the check. The identity, the sweep, and the seed all
land or none do — an import that fails must leave the device exactly as it was.

### 3.2 The sender could take the rebate, and that is accepted

In claim-kit mode the sender *generates* the recipient's identity key, so a
sender who keeps a copy can compute `z` and collect the rebate before the
recipient does. The same copy also lets them sweep `T`.

This is inherent to sender-provisioned identities and predates this plan — a
sender in this mode can already read the secret they sent, which is strictly more
than the rebate is worth. The rebate gives the position a monetary edge it did
not previously have, which is why it is written down.

**Ruled not concerning, 31 July 2026**: where the recipient is an individual the
sender is someone they already trust with the payload, and the amounts are
trivial. No mitigation is planned. Noted so that a later reader does not mistake
the silence for an oversight, and so it can be revisited if kit-mode is ever used
at scale by an organisation provisioning identities on others' behalf — where the
trust assumption is materially different.

## 4. Out of scope: the general case and the bounty

Two callers are deliberately **not** solved here, and need their own plan
(owner ruling, 31 July 2026 — the reporter/bounty scope is not this plan's):

- **A recipient who already holds an identity key** and is sent a secret without
  a kit. Route A cannot reach them: there is no kit to seed.
- **The early-reveal reporter** (§1.2), whose window cannot be waited out and for
  whom no client-side courtesy exists.

The shape that would cover both, recorded so it is not re-derived: let the
**beneficiary differ from the signer**. Today the address receiving the payment
must be the address signing for it, which is the defect itself. Split those
roles and anyone may submit and pay the fee while the payment lands at — and so
creates — the new account.

For the rebate this stays safe by changing only *what the commitment binds*: from
the submitter's address to the beneficiary's. A submitter then sees the whole
proof and still cannot redirect a penny; they can only decline to submit. For the
bounty the fields already exist — `MsgSlashGuardian` carries `reporter_address`
separately, and only the `signer` option ties them together; the evidence is
self-authenticating (a guardian's own HMAC-verifiable share), so *who* submits was
never load-bearing.

Two costs that plan must accept rather than discover: the submitter learns the
recipient received a secret, and permission is not incentive — a reporter with
minutes and nobody willing to submit is still stuck.

## 5. Route C — ask a person (already merged)

Shipped in the rebate collection UX plan §2.8: below the cost of collecting, the
rebate surfaces explain the situation and offer a shareable request for 0.1 VEIL.
Correct whenever the recipient has somebody to ask, and no help when they do not.
Retained under either route above.

## 6. Rulings

Recorded because each shaped the design and none is re-litigated: the recipient
stays the beneficiary (§2); a funded kit is the default rather than a checkbox
(§3); the seed is 0.2 VEIL, derived (§3); no reclaim affordance (§3); a kit-mode
sender taking the rebate is accepted (§3.2); the general case and the bounty are
out of scope (§4). All 31 July 2026.

## 7. Not solved

- **The fully cold start.** Someone with no funded kit and nobody to ask still
  cannot transact. §4 names the shape that would reach them and hands it on.
- **Pre-launch funding**, which remains an operator concern from the
  bootstrapping pool.
- **Two overstated claims, corrected as a deliverable here rather than left to
  be noticed later**: the recipient rebate plan's premise that the rebate "is how
  a new wallet gets its first VEIL on every network", and the Funding screen copy
  promising the same. Both are true only for a wallet that can already transact.

## 8. Work items

No spec change leads this one: route A alters no protocol behaviour. The claim-kit
URI format IS a compatibility surface between sender and recipient, so it is
versioned rather than silently extended — an older app must reject a seeded kit
it cannot sweep instead of dropping the funds on the floor.

1. Kit URI format (§3.0): carry an optional courier key under `tfck` in a `seed`
   parameter, payload version 2 when present and 1 when absent. Encode and decode
   in `typescript-sdk`, with `CLIENT_CONVENTIONS.md` §4 updated and the
   append-only vector corpus extended — the format is an ecosystem convention, so
   it does not change without both.
2. Creation: generate `T`, fund it, default on in kit mode, and show the seed in
   the cost breakdown alongside the shortfall path when the balance cannot cover
   it (§3).
3. Import: refuse a seeded kit with no valid wallet address to sweep into
   (§3.1a), then sweep `T` to that wallet and treat `T` as spent. Idempotent — a
   second import must not attempt a second sweep of an emptied courier, nor
   present an already-empty courier as a failure.
4. Copy: state at creation that the seed travels with the kit and does not come
   back (§3), and correct the two overstated claims in §7.
5. Tests: kit round-trip seeded and unseeded; an old-version payload rejected
   rather than partially read; a seeded kit refused with no wallet, and nothing
   persisted by the failed attempt; sweep arithmetic (seed minus the send fee);
   the idempotent second import; and the assertion that a courier key never
   appears in any on-chain message.
6. A devnet scenario proving a **never-funded** recipient collects a rebate end
   to end. This is the case no existing suite covers — every current path,
   including the rebate drill, collects with a pre-funded devnet account, which
   is exactly why the defect survived to production.
