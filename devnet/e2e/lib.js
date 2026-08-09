/**
 * Shared devnet wiring for the e2e driver — the one place endpoints, wallets,
 * the recipient keypair and the WASM crypto backend are assembled. Everything
 * here composes the PUBLIC timeflare-sdk surface, except the raw-WASM escape
 * hatch (loadWasmPrimitives), which exists only for the scenario tooling's
 * manual seal and is deliberately not part of the SDK API.
 */

const fs = require('fs');
const path = require('path');
const url = require('url');
const { execSync } = require('child_process');

const sdk = require('@timeflareio/typescript-sdk');
const { createWasmCryptoProvider } = require('@timeflareio/typescript-sdk/wasm');

const CHAIN_ID = process.env.CHAIN_ID || 'timeflare-test';
const RPC = process.env.RPC_ENDPOINT || 'http://localhost:26657';
const REST = process.env.API_ENDPOINT || process.env.TIMEFLARE_REST || 'http://localhost:1317';
const TIMEFLARE_HOME = process.env.TIMEFLARE_HOME || `${process.env.HOME}/.timeflare`;

// The devnet test user — created and funded from the bootstrapping pool by
// devnet/users/setup-test-users.sh, whose USER_NAME this must match.
const USER_NAME = process.env.TIMEFLARE_USER_NAME || 'user';

/** Block time (seconds) from TIMEFLARE_BLOCK_TIME; drives poll cadence. */
function blockSeconds() {
  const raw = String(process.env.TIMEFLARE_BLOCK_TIME || '6s').trim();
  const parsed = parseFloat(raw);
  if (!Number.isFinite(parsed) || parsed <= 0) return 6;
  return raw.endsWith('ms') ? parsed / 1000 : parsed;
}

/** Load the devnet test user as a signing wallet via the CLI export. */
async function loadUserWallet() {
  const passphraseFile = `${TIMEFLARE_HOME}/user/keyring_passphrase`;
  const keyringDir = `${TIMEFLARE_HOME}/user/keyring`;
  if (!fs.existsSync(passphraseFile)) {
    throw new Error('user keyring not found — run: ./devnet/users/setup-test-users.sh fund');
  }
  // The file content IS the passphrase, verbatim (raw-only ruling — the
  // key-management consolidation plan; never base64-decoded).
  const passphrase = fs.readFileSync(passphraseFile, 'utf8').trim();
  const privKeyHex = execSync(
    `echo "${passphrase}" | timeflared keys export ${USER_NAME} --unarmored-hex --unsafe ` +
      `--keyring-backend file --keyring-dir ${keyringDir} -y 2>/dev/null | tail -1`,
    { encoding: 'utf8' },
  ).trim();
  return sdk.walletFromPrivateKeyHex(privKeyHex);
}

/**
 * Where the devnet's recipient keypair lives.
 *
 * There is no sensible default: the driver runs against a native devnet or the
 * compose stack, and only the make target that invokes it knows which — so the
 * target passes RECIPIENT_KEYPAIR. Failing loudly beats reading a file that
 * happens to exist at a guessed location.
 */
function recipientKeypairPath() {
  const p = process.env.RECIPIENT_KEYPAIR;
  if (!p) {
    throw new Error(
      'RECIPIENT_KEYPAIR is not set.\n' +
      'The driver runs against a devnet, whose location it cannot infer.\n' +
      "Set it to the devnet's recipient keypair, e.g.\n" +
      '  RECIPIENT_KEYPAIR=.devnet/recipient-keypair.json node devnet/e2e/secret-lifecycle.js\n' +
      '`make e2e` sets this for you.',
    );
  }
  return p;
}

/** The recipient's X25519 public key from the devnet keypair file. */
function loadRecipientPublicKey() {
  const data = JSON.parse(fs.readFileSync(recipientKeypairPath(), 'utf8'));
  return new Uint8Array(Buffer.from(data.publicKey, 'hex'));
}

/** The recipient's full keypair (public + private) — for reconstruction/discovery. */
function loadRecipientKeypair() {
  const data = JSON.parse(fs.readFileSync(recipientKeypairPath(), 'utf8'));
  return {
    publicKey: new Uint8Array(Buffer.from(data.publicKey, 'hex')),
    privateKey: new Uint8Array(Buffer.from(data.privateKey, 'hex')),
  };
}

/** Build the connected clients + WASM crypto the driver scripts share. */
async function connect() {
  const wallet = await loadUserWallet();
  const rest = new sdk.TimeflareRestClient(REST);
  const tx = await sdk.TimeflareTxClient.connect(RPC, wallet.wallet);
  const crypto = await createWasmCryptoProvider();
  return { wallet, rest, tx, crypto, address: wallet.address };
}

/**
 * Escape hatch for the scenario tooling ONLY: the raw wasm-bindgen module,
 * exposing the audited low-level primitives (encrypt_with_public_key,
 * generate_keypair, split_secret, generate_guardian_hmac) the high-level
 * seal composes. Needed because the scenario suite must capture the PLAINTEXT
 * key-share envelopes (early-reveal evidence) that sealSecret discards. Not
 * part of the public SDK surface.
 */
async function loadWasmPrimitives() {
  // The WASM bundle is the @timeflareio/crypto dependency, so ask the resolver
  // where it landed. The wasm-bindgen (web target) module is ESM, hence the
  // dynamic import from CJS.
  const jsPath = require.resolve('@timeflareio/crypto');
  const wasmPath = path.join(path.dirname(jsPath), 'timeflare_crypto_bg.wasm');
  const mod = await import(url.pathToFileURL(jsPath).href);
  await mod.default(fs.readFileSync(wasmPath));
  return mod;
}

module.exports = {
  CHAIN_ID,
  RPC,
  REST,
  blockSeconds,
  loadUserWallet,
  loadRecipientPublicKey,
  loadRecipientKeypair,
  connect,
  loadWasmPrimitives,
  sdk,
};
