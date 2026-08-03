# Rebate Collection UX (mobile) — Plan

*Collecting a rebate is two transactions a block apart. The card should carry
the recipient across that gap by itself, and it should say something true when
there is nothing to collect yet.*

> **Status: done — 30 July 2026**, branch `worktree-rebate-collection-ux`, PR #118.
>
> **Priority**: P2 — the rebate is the recipient's only funding path, and the
> collection flow currently strands the user mid-way through a two-step
> transaction they have already paid for.
>
> **Origin**: owner observation, 30 July 2026, testing collection on the native
> devnet: *"the 2-transaction claim/collect rebate doesn't have a great UX. We
> need to observe the first transaction and gracefully advance to 'Collect
> rebate'. Otherwise the user has to navigate away, and come back in, just to
> click again."* Then, on the observed behaviour: *"I just get repeating the
> commit cycles. We show commit once, have some pending/loading experience for
> them to then claim."* Two further gaps found while diagnosing it (§2.2, §2.3).
>
> **Rulings, 30 July 2026**: the flow is **one button tap** that carries the
> recipient through both transactions automatically, with an unmistakable
> pending state and copy telling them to stay on the screen (§2.1); the two
> steps keep the names **Claim** and **Collect** as progress labels (§2.5).
>
> **Components**: `mobile-client/app/src/screens/inbox/RebateCard.tsx`,
> `mobile-client/app/src/screens/inbox/useInboxItems.ts`,
> `mobile-client/app/src/screens/onboarding/WalletScreen.tsx` (§2.6),
> `mobile-client/app/src/state/rebate.ts`, a new shared collection hook (§2.6),
> a funding-prompt component (§2.8),
> `mobile-client/app/test/rebate.test.ts`. No chain, proto, spec or SDK change —
> this is presentation over an unchanged protocol.
>
> **Bounded by**: [DONE_RECIPIENT_REBATE_PLAN.md](DONE_RECIPIENT_REBATE_PLAN.md),
> which owns the mechanism itself. A separate plan is needed for the
> bootstrapping defect that plan's premise assumes away (§5).

## 1. Problem

Collection is commit–reveal: `MsgRecipientCommitRebate`, then
`MsgRecipientCollectRebate` in a strictly later block. The card models this as
four phases (`collectable` → `committed` → `revealable` → `collected`) and
renders a button in two of them.

The transition from `committed` to `revealable` never happens on screen.
`rebatePhase` advances when `currentHeight > committedAtHeight`, and
`currentHeight` is the `chainHeight` prop, fed from `useIncomingSecret`'s
`lastSyncedHeight`. That hook probes on focus only (`useInboxItems.ts:261`),
plus on reconstruction events. So the height is sampled *before* the commit is
broadcast and never resampled while the screen is open: the card sits on
"Committed at block N. The next block makes it collectable." indefinitely.

The user's recovery is to navigate away and back, which forces a refocus probe.
Nothing warns them that this is what is required.

**The two defects compound into a loop.** The commitment lives in React state
(§2.2), so the navigation that fixes the frozen height is also the navigation
that discards the commitment. Returning to the card, it reads `collectable`
again and offers the first step — so the user commits a second time, waits, is
stranded again, navigates again, and commits again. Each cycle costs another
9,000 uveil and never reaches the reveal. This is the reported symptom, and it
is why §2.1 and §2.2 must land together: either alone leaves the loop intact.

The step must therefore be offered **once**, with the wait presented as a
pending state rather than as a button that invites a repeat tap.

## 2. What changes

### 2.1 One tap, carried through both transactions

A single **Claim rebate** tap runs the whole sequence: commit, wait for the
block, collect. The recipient taps once and watches.

- After the commitment lands, poll `rest.height()` until it exceeds the
  commitment height, then broadcast the collection automatically.
- **Interval**: 6s, matching `CHAIN_POLL_MS` on the home screen and the chain's
  block time. Reuse that constant rather than introducing a second cadence.
- **Shape**: chained `setTimeout`, not `setInterval`, so a slow response cannot
  stack probes — the same pattern the home screen's poll already uses.
- **Lifetime**: runs only while a collection is in flight. Stopped on unmount, on
  completion, and on failure. A card with nothing pending must not poll.

**The button is shown once.** From the tap until the rebate is collected, no
control offering Claim is on screen — the pending state stands in its place. The
only route back to Claim is the chain reporting no commitment for this address
(§2.7). That removes the repeat-tap loop by construction rather than by asking
the user to wait patiently.

**The pending state must be unmistakable, and must say to stay put.** A spinner
or equivalent activity affordance, the step being performed named in the
recipient's vocabulary (Claiming… → Collecting…), and an explicit line asking
them to remain on the screen while it completes. The wait is roughly one block —
about 6 seconds — but it is two transactions and can be slower, and a static
"Committed at block N" line reads as finished when it is not.

**Leaving is safe, even though we ask them not to.** The persisted commitment
(§2.2) means navigating away or backgrounding the app cannot lose the money — it
only stops the automation. On return with a commitment already aged past its
block, the card offers **Collect** as a one-tap action rather than firing
automatically: auto-broadcasting a fee-bearing transaction on mount, for a tap
made in an earlier session, is a surprise. Automation belongs to the flow the
user initiated and is watching; resumption is explicit.

### 2.2 Persist the commitment

`RebateCard.tsx:46` holds the commitment in `useState`. Backgrounding the app,
an iOS kill, or navigating away discards it, and the card falls back to
`collectable` — offering "Collect rebate", which re-commits and charges a second
fee (9,000 uveil) for a commitment already on chain.

It is recoverable rather than fund-losing: `RebateCommitments.Set` overwrites, so
a second commit resets `committed_at` and collection proceeds a block later. But
it is a wasted fee and a confusing round trip, and the app already has `kv` in
scope for the identity lookup.

- Persist `{secretId, committedAtHeight}` to `kv` on commit; rehydrate on mount.
- Clear on successful collection.
- Drop a stored commitment whose secret's collection deadline has passed, so the
  store cannot accumulate dead entries.

Client-side only, deliberately. The chain records the commitment but exposes no
query for it (`query.proto` has no `RebateCommitment` RPC), so chain-side
recovery would mean a protocol-surface change. That would additionally survive a
device restore, which the local store does not — noted as residual in §4 rather
than solved here.

### 2.3 Say why the card is empty

`RebateCard` returns `null` whenever `phase.kind === 'none'`, which collapses
four distinct situations into an identical blank:

| Situation | Resolves? |
|---|---|
Settlement has not happened yet (`rebate_amount` null before `reveal_end_block + 1`) | Yes, on its own |
Rebate suppressed below the 0.05 VEIL dust floor | **Never** |
Secret not in the inbox (no hint scan) | Only via discovery or manual add |
No native crypto module, so no scan ran | Not without a different build |

The first two are the card's own business and are distinguishable from data it
already holds: `revealEndBlock` versus the current height separates
"not yet assessed" from "assessed at nothing".

- Before settlement: state that the rebate is assessed when the reveal window
  closes, and name the block.
- After settlement with no credit: state plainly that this secret's rebate fell
  below the minimum and there is nothing to collect. A permanent nothing must
  not look like a pending something.

The two inbox-level causes are out of scope — they affect every incoming secret,
not just rebates, and belong with whatever owns the silent-scan gap.

### 2.4 Move the privacy warning ahead of the tap — prerequisite for §2.1

Found while tracing the phases. The card warns —

> *"Revealing publishes the proof. It links this address to this secret,
> permanently — the protocol has no private way to collect."*

— only when `phase.kind === 'revealable'`, which is **after** the commitment has
been signed and paid for. The user commits before being told what collecting
costs them in privacy.

With §2.1's single tap this stops being an ordering nicety and becomes a
correctness requirement. Auto-collection removes the second decision point
entirely, so the *only* moment the recipient can give informed consent to
publishing the proof is before the one tap they make. A warning shown after that
tap would describe something already irreversible.

The warning therefore sits with the Claim button, before it is pressed, and §2.4
lands before §2.1 in the work order.

### 2.5 Name the two steps Claim, then Collect

The buttons currently read "Collect rebate" and then "Finish collecting" — one
verb split across two steps, so neither label distinguishes them and the second
reads as a continuation of something the user thought they had already done.

The steps are **Claim** then **Collect**. Claim registers the intent (the
commitment); Collect takes the money (the reveal and payment). Two verbs, in the
order they happen.

Since §2.1 makes this one tap rather than two, the two names survive as the
**progress labels inside the pending state** — "Claiming…" then "Collecting…" —
with the single button reading **Claim rebate**. The recipient still sees both
steps and their order; they simply are not asked to drive each one. The one place
"Collect" appears as a button is the explicit resumption path (§2.1) and the
failure retry (§2.7).

The mechanism keeps its name — this is the **rebate** card, and the surrounding
copy stays "rebate" throughout. Note for the record that the July 2026 ruling
which chose "rebate" over "claim" did so to avoid confusion with the **claim
kit**; this reintroduces "Claim" as a step label inside the rebate flow, ruled
30 July 2026 on the grounds that it is the clearer verb for the first step and
the two features are not adjacent in the UI. Should that confusion surface in
use, "Reserve" is the fallback with no other meaning in the product.

Internal phase identifiers (`collectable`, `committed`, `revealable`,
`collected`) stay as they are: they mirror the protocol's commit–reveal and are
pinned by the existing tests. This is a copy change, not a rename.

### 2.6 A collectable-rebates list on the Wallet screen

Today a rebate is only reachable by finding the secret that earned it in the
Inbox and opening it. That is the wrong entry point for the money: someone
topping up their wallet is thinking about a balance, not about which secret
produced it.

The Wallet screen gains a **Rebates to collect** section listing every rebate
this device can collect, with the same Claim → Collect flow available in place.

- **No secret information.** Per entry: the amount, the expiry, and the action.
  No title, sender, identity name, or payload — the entry is a sum of money, not
  a message. The Inbox remains the only place a secret is described.
- **No secret identifier either.** Amount and expiry distinguish entries
  adequately, and a visible ID invites the reader to correlate. (The ID is public
  on chain, so this is presentation hygiene rather than a privacy guarantee.)
- **A section total**, so "top up" has a headline figure rather than requiring
  mental arithmetic across rows.
- **Empty state**: state plainly that there is nothing to collect. This section
  is empty for most users most of the time and must read as normal, not broken.
- **Ordering**: soonest expiry first. The only urgency a rebate carries is its
  three-month deadline, so the list should surface what is closest to being lost.

**One implementation, two surfaces.** The collection flow — phase derivation,
persisted commitment, polling advance, the two transactions, error handling — is
extracted into a shared hook (`useRebateCollection(secretId)`) that both the
Inbox card and the Wallet rows consume. Neither surface reimplements any part of
it. Duplicating the flow would put two copies of a commit–reveal sequence in the
app, and they would drift on exactly the details that cost users money; CLAUDE.md
treats two implementations of one concern as a defect, and this is one.

**Data is a projection, not a new store.** Collectability needs the credited
amount and collected flag (already on `SecretView`), the hint's ephemeral key and
the identity that discovered the secret (already on the inbox records). So the
list filters existing inbox state — credited, uncollected, unexpired, and
provable on this device — and introduces no new persistence.

A consequence worth stating: a rebate on a secret the inbox never discovered does
not appear here either. This section inherits the discovery gap in §4, it does
not work around it.

### 2.7 The failure path never offers Claim again

If the poll cannot reach the chain, or the Collect transaction fails, the card
holds its position and offers **Retry collect**. The persisted commitment (§2.2)
means the first step genuinely does not need repeating, and reverting to Claim
would recreate the loop this plan exists to remove.

Claim returns only when there is no commitment for this address — the one case
where re-committing is the correct action rather than a wasted fee.

The dust-floor copy (§2.3) names no figure. "Below the minimum" is enough;
quoting 0.05 VEIL invites arithmetic the recipient cannot act on, and the
constant may be retuned by a future upgrade.

### 2.8 When the wallet cannot pay for its own collection

Collection costs two transaction fees, and they are paid **before** the rebate
arrives. A wallet that has never received anything cannot pay them — and cannot
sign at all, since Cosmos creates an `auth` account record only on first
receipt, so there is no account number or sequence to sign against (§5).

Offering Claim in that state is offering a button that cannot work. Instead the
card explains the situation and helps the recipient get funded.

**The gate is the balance, not account existence.** `rest.balance()` returns `0`
for an address with no account, so one comparison — balance below the cost of
collecting — catches both cases that matter: no account (cannot sign) and an
account too poor to pay fees. The remedy is identical, so distinguishing them
would change nothing the user does. (The auth endpoint *can* tell them apart,
returning `code 5, account … not found`; deliberately not used, since it would
mean a new SDK method for a distinction with no consequence.)

**The ask is derived, not hardcoded**: four times the cost of the two
transactions, rounded up to the nearest 0.01 VEIL. At the pinned gas constants
that is **0.1 VEIL** — 90,000 gas for the commit and 140,000 for the reveal, so
23,000 uveil at the 0.1 uveil floor price. Deriving it means a gas retune moves
the ask instead of leaving a stale figure that silently under-funds people.

Four times, rather than exactly enough, because a second round trip to the
sponsor is far worse than asking for 4x a negligible sum: it absorbs a retry of
either step, and a node configured above the floor gas price. Anything left over
stays in the wallet toward sealing a first secret, which is the point of the
mechanism.

**Sharing, not copying.** The prompt offers the request text through React
Native's core `Share` — no new dependency, and "ask someone to fund me" is
inherently a send-to-a-person action, so it opens the recipient's messaging app
with the text ready rather than making them find somewhere to paste. The same
text renders as selectable monospace beneath it as a manual fallback.

This deliberately avoids `Clipboard`: the core module is deprecated, its copies
fail silently on iOS, and replacing it needs a dependency decision that is not
this plan's to take. A copy-based prompt would ship a button that does nothing
on the platform the flow is being tested on.

**The request text discloses nothing.** It asks for the amount, gives the
address, and explains to the sponsor why a new wallet needs a first payment. It
does not mention the rebate: that would tell the reader this person received a
secret. Disclosing it to a chosen sponsor is the recipient's call, not a default
the app puts in their mouth.

**One prompt per surface, not per row.** Funding is a property of the wallet, so
the wallet list shows it once above the rows rather than repeating it on each.
Both surfaces suppress Claim while it stands, through the same
`rebateControls` decision.

## 4. Not solved

- **Commitment recovery across a device restore.** The local store covers app
  restarts, not a reinstall. Closing that needs a `RebateCommitment` query RPC —
  a protocol-surface change, deliberately excluded.
- **The empty inbox with no native crypto module.** `useInboxItems` skips the
  discovery scan when the module is absent and reports nothing; a secret never
  discovered has no card to explain itself. Belongs to the inbox, not here.
- **The bootstrapping defect.** None of this helps a wallet with no on-chain
  account — see §5. Better UX over an impossible action is worse than none. The
  remedy sits with the owner (ruled 30 July 2026); this plan neither solves nor
  presumes a solution.

## 5. The defect this plan does not touch

A never-funded address cannot collect a rebate at all. Cosmos creates an `auth`
account record only when an address first *receives* funds; without it there is
no account number or sequence, so no transaction can be built. Verified on the
native devnet, 30 July 2026, with a freshly generated key:

```
account tmflr14yvy7mldnhsn7vyxkdzvefzsx6yepzsv0mk0m3 not found: key not found
```

The fees themselves are not the obstacle — 23,000 uveil for both transactions
against a rebate that must clear 50,000 — but they must be paid *before* payment
arrives, from an account that must already exist.

This contradicts the premise of
[DONE_RECIPIENT_REBATE_PLAN.md](DONE_RECIPIENT_REBATE_PLAN.md) ("this is
how a new wallet gets its first VEIL on every network") and the Funding screen's
copy. That plan's §9 defers "claiming to an address other than the signer's" on
the grounds that signing *is* the proof of control — which is sound, and is also
precisely the constraint that locks out a new wallet.

**The remedy is deferred to the owner** (ruled 30 July 2026): the problem is
recorded, no approach is adopted, and nothing in this plan depends on how it is
eventually solved. Sponsored collection was raised as one candidate and is
explicitly *not* endorsed here — it would be a protocol change needing its own
plan.

Recorded so this plan is not mistaken for closing it, and so the two claims that
are currently false are not forgotten: the rebate plan's premise, and the Funding
screen copy promising a rebate as a funding route for a wallet with nothing in
it.

## 6. Work items

1. §2.2 — persist and rehydrate the commitment; clear on collection; drop past
   the deadline. **First**, because it is the half of the loop that costs money,
   and every later item assumes the commitment survives.
2. §2.6 — extract the collection flow into the shared hook and move the Inbox
   card onto it, with no behaviour change. Done **before** the new behaviour
   below, so §2.1 and §2.7 are written once rather than ported.
3. §2.4 — the privacy warning moves to sit with the Claim button. Before §2.1,
   because auto-collection removes the later consent point it currently relies on.
4. §2.5 — Claim / Collect copy, single button, progress labels.
5. §2.1 — one tap through both transactions; 6s chained poll; button shown once;
   unmistakable pending state with stay-on-screen copy; explicit resumption.
6. §2.3 — distinguish "not yet assessed" from "assessed at nothing".
7. §2.7 — the failure path: Retry collect, and Claim only when the chain has no
   commitment for this address.
8. §2.6 — the Wallet section: filtered list, section total, expiry ordering,
   empty state, rows driven by the shared hook.
9. §2.8 — the funding prompt: balance gate, derived ask, Share plus selectable
   fallback, Claim suppressed while it stands.
10. Tests in `app/test/rebate.test.ts`: rehydration round-trip, deadline-expiry
   discard, the two empty-state branches either side of `revealEndBlock`, and
   the advance from `committed` on a height increment. The phase machine is
   already headless and unit-tested — these extend that suite rather than
   needing a new one. Add one regression test asserting that a card holding a
   commitment never offers Claim, since that is the loop. Assert the automation
   itself: one tap drives commit → wait → collect with no further input, the
   collection fires only once however many height ticks arrive, and a rehydrated
   commitment from an earlier session does **not** auto-fire but offers Collect.
   For §2.6, test the filter and ordering headlessly (credited, uncollected,
   unexpired, provable on this device; soonest expiry first) and assert the
   section total sums the listed rows.
11. Verify on the native devnet against a real settled secret: one Claim tap,
    remain on screen, and observe both transactions complete with no further
    input. Then repeat, backgrounding the app mid-wait, and confirm it returns
    offering Collect — not Claim, and not a silent broadcast on mount. Repeat
    from the Wallet section, and confirm the balance and the section total both
    move after collection. `REBATE_COLLECTION_DRILL=1` produces a secret whose
    rebate clears the dust floor.

No `docs/spec.md` change: nothing here alters protocol behaviour.
