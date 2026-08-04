# Networks

`networks.json` at the repository root is the single definition of the networks
this chain runs as — the local devnet today, testnets and mainnet as they come
into existence. It is **published for reading**: a client, a daemon or a script
fetches it, parses it, and uses it to establish which network to connect to and
what values that connection needs. Nothing downstream keeps its own copy of a
chain id, a port or an address prefix.

It is read from:

```
https://raw.githubusercontent.com/timeflareio/chain/main/networks.json
```

That URL is where it lives today, not where it lives permanently — expect it to
move to a host of the project's own. A consumer should hold the location in one
place so following that move is a one-line change.

It is deployment fact, not protocol. [spec.md](../spec.md) remains the authority
for what the protocol *does*; this file only records where an instance of it
answers, and under what identity. Nothing here is consensus-relevant, a change
to it never needs a protocol release train, and it is not pinned to a release —
a network appears in it when it comes into existence, which has nothing to do
with when the chain ships software.

## What it is for

To be read and used, by anything that needs to reach a Timeflare network:

- **Establishing a connection.** The endpoints and the chain id come from here,
  so a consumer resolves a name (`devnet`, a testnet) into an address rather
  than hard-coding one.
- **Setting derived values.** The address prefix, the chain id to assert before
  signing, and whether the gRPC dial takes TLS all come out of the same entry.
- **Following a change.** A network that appears after a build shipped is
  offered by it, and an endpoint that moves is followed, without releasing
  anything.

**What it provides are defaults.** It is served over plain HTTPS and is not
signed, so a consumer should treat it as a convenience and never as an
authority that overrides its own operator or user: an explicitly configured
endpoint always wins, and a means of entering one by hand always remains. A
client that cannot reach it must still be able to reach the chain, so keep a
fallback — the copy shipped with the build, or the last one read.

## The structure

```json
{
  "default": "devnet",
  "addressPrefix": "tmflr",
  "networks": [
    {
      "id": "devnet",
      "label": "Local devnet",
      "chainId": "timeflare-test",
      "local": true,
      "endpoints": {
        "rpc": ["http://localhost:26657"],
        "rest": ["http://localhost:1317"],
        "grpc": ["localhost:9090"],
        "tls": false
      }
    }
  ]
}
```

### Top level

| Field | Meaning |
|---|---|
| `default` | The `id` of the network a consumer selects when the operator or user has not chosen one. Exactly one, and every consumer honours the same one. |
| `addressPrefix` | The bech32 account prefix, project-wide rather than per-network. Mirrors `AccountAddressPrefix` in `app/app.go`, which remains the definition; `make verify-networks` fails if the two drift. |
| `networks` | Every network, each carrying the same fields. |

### A network

| Field | Meaning |
|---|---|
| `id` | Stable machine name, unique across the list. What `default` refers to and what a consumer persists when a user picks a network. |
| `label` | Human-readable name for a settings screen. |
| `chainId` | The id the node reports, and the one a client must assert before signing. Never absent: a network that cannot be identified cannot be signed for safely. |
| `local` | Whether the network is loopback-scoped — see below. |
| `endpoints` | `rpc`, `rest` and `grpc`, each a list, plus `tls`. |

## Why three endpoints

`grpc` (9090) and `rest` (1317) are **the same query service in two encodings**.
Every method in `proto/timeflare/secrets/v1/query.proto` carries a
`google.api.http` annotation, so grpc-gateway transcodes the whole service into
JSON routes. A consumer uses whichever its runtime can speak: Go clients take
native gRPC, while browser and React Native runtimes cannot — gRPC needs HTTP/2
trailer frames their fetch stacks do not expose — so they take REST.

`rpc` (26657) is not a third encoding of that service. It is CometBFT's own
endpoint, one layer below: block and header reads, transaction broadcast, and
the websocket event subscription. Both a Go daemon and a JavaScript client need
it, because neither 9090 nor 1317 offers events or broadcast.

Their forms differ because their client libraries differ: `rpc` and `rest` are
URLs, `grpc` is `host:port`, which is what cosmos gRPC clients accept. Each
value is written in the form its own consumer passes straight through.

**`tls` is what a gRPC address cannot say.** An `rpc` or `rest` URL states its
own transport in its scheme; `host:port` has nowhere to put it, and a gRPC
client must decide between transport credentials and an insecure dial before it
connects. So the flag carries it: `"tls": false` dials insecure, `true` dials
with TLS. It tracks `local` — cleartext on the machine running the node, TLS
everywhere else — and `make verify-networks` fails if the two disagree.

**An empty list is a statement.** Plenty of deployments expose 1317 and not
9090. `"grpc": []` records that directly, so a daemon that needs gRPC learns at
configuration time that it cannot run against that network, rather than at its
first dial.

## `local`, and the host a consumer cannot be told

`local: true` means the network is reachable only from the machine running the
node. Two things follow.

**Cleartext is permitted here and nowhere else.** A loopback endpoint may stay
`http://`; anything off the machine must not be, because guardian encryption
keys are fetched over the REST endpoint and shares are sealed to them — whoever
can rewrite that response chooses who can read the secret.

**The host is the consumer's to substitute.** The endpoints are written as seen
from the machine running the node, which is what a guardian and an iOS simulator
use verbatim. A runtime that cannot reach `localhost` rewrites the host and
keeps the rest: the Android emulator reaches its host as `10.0.2.2`, and a
physical device needs the LAN address its user provides. That mapping belongs to
the consumer, because the same node has a different address from each of them —
this file cannot state one that is true for all.

## Adding a network

Append an entry with the same fields. A public network carries real endpoints
and no host substitution:

```json
{
  "id": "testnet-1",
  "label": "Public testnet",
  "chainId": "timeflare-testnet-1",
  "local": false,
  "endpoints": {
    "rpc": ["https://rpc.testnet.example.org"],
    "rest": ["https://api.testnet.example.org"],
    "grpc": ["grpc.testnet.example.org:443"],
    "tls": true
  }
}
```

Endpoints are lists so that one unreachable host is not an unreachable network;
list every host that serves the same chain id.

Only the devnet is defined today, because it is the only network that exists. A
testnet row lands here when there is one to name — deliberately not before, since
an entry naming a host that does not answer would be handed to consumers as a
default.

## What is checked

`make verify-networks` runs as part of `make verify` and enforces:

- the file is valid JSON, and `default` names a network that exists;
- ids are unique, and every entry carries all six fields;
- every `chainId` is a well-formed chain id;
- `local` entries use a loopback host, and non-local entries use `https` for
  `rpc` and `rest`;
- `tls` is present on every entry and is the opposite of `local`;
- `addressPrefix` matches `AccountAddressPrefix` in `app/app.go`;
- the devnet `chainId` matches the default `CHAIN_ID` in
  `devnet/lib/common-utils.sh`.

The last two are the point of the file rather than incidental tidiness: they are
the values that were previously copied by hand into other repositories, and the
check is what stops this definition and its origin drifting apart.

## Consuming it

Fetch it, parse it, resolve the entry you want, and configure from that entry.
Four things are worth doing whatever the consumer:

1. **Hold the URL in one place.** It will move off GitHub, and that should be a
   one-line change rather than a search.
2. **Keep a fallback** — the copy shipped with the build, or the last one read
   and cached. Something that cannot reach the published list must still reach
   the chain, and nothing should block startup on fetching it.
3. **Validate before use, shallowly.** Check the fields you read and that
   `default` names an entry that exists. Accept fields you do not recognise: a
   list carrying something new is a newer chain talking to an older consumer,
   which is the ordinary case over time rather than an error.
4. **Let explicit configuration win.** An operator's endpoint or a user's choice
   overrides whatever the list says, and is never silently replaced by a
   refresh.

Do not hand-copy entries into source. A copied constant is the problem this
file exists to remove.
