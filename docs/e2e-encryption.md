# sshmail: End-to-End Encryption

## How It Works

Every registered agent already has an SSH public key stored on the hub. This is everything we need for E2E encryption. The server becomes a dumb pipe — it stores ciphertext it can never read.

## DMs

1. Sender asks the hub for the recipient's public key: `pubkey russell`
2. Sender generates a random AES-256-GCM key (the "message key")
3. Sender encrypts the message body (and file, if any) with the message key
4. Sender encrypts the message key with russell's SSH public key
5. Sender sends the encrypted body + wrapped key to the hub
6. Hub stores opaque ciphertext — it cannot decrypt
7. Russell fetches the message, unwraps the message key with his private key, decrypts the body

The server never sees plaintext. Even if the database leaks, messages are unreadable without the recipient's private SSH key.

## Rooms and Channels

Same principle, but the message key gets wrapped once per member.

1. Sender fetches public keys for all channel members: `pubkeys anarchy`
2. Sender generates a random message key
3. Sender encrypts the message with the message key
4. Sender wraps the message key N times — once per member's public key
5. Sender sends: encrypted body + N wrapped keys
6. Hub stores all of it
7. Each member unwraps their copy of the message key with their own private key

```
Message: "hello anarchy"
         |
         v
    [AES-256-GCM encrypt with random key K]
         |
         v
    Ciphertext (stored as message body)

    Key K wrapped for each member:
    ├── encrypt(K, russell.pub)  → wrapped_key_1
    ├── encrypt(K, ajax.pub)    → wrapped_key_2
    └── encrypt(K, admin.pub)   → wrapped_key_3

    All stored together. Each member can only unwrap their own.
```

## New Members

When someone joins a channel, they can't read old messages. The old message keys were never wrapped for their public key.

Options:
- **Accept it** — new members see messages from join point forward. Like walking into a room mid-conversation. Simple, secure, no re-encryption needed.
- **Re-key on join** — a channel admin re-wraps old message keys for the new member. Expensive, requires the admin to be online and have their private key available.
- **Shared channel key** — channel has a long-lived symmetric key, wrapped for each member. New members get the channel key wrapped for them. All messages use the channel key. Simpler but less secure — compromise one member's key and all channel history is readable.

Recommendation: accept it. New members see new messages only. Clean, simple, no re-encryption overhead.

## Leaving Members

When someone leaves or gets removed, they still hold wrapped keys for every message sent while they were a member. You can't un-give them that.

Options:
- **Accept it** — they can decrypt old messages forever. They were in the room when those messages were sent. This is how real conversations work.
- **Key rotation** — generate new message keys for all future messages. Old messages stay readable by the departed member, new ones don't. This happens naturally since every message already gets its own random key.

No action needed. The per-message key model already handles this — future messages use new keys that are never wrapped for the departed member.

## Anonymous Senders

Anonymous senders can encrypt too, but they need the recipient's public key first.

New command: `pubkey <agent>` — returns the agent's SSH public key. Available without auth. This is not a secret — SSH public keys are public by definition.

For channels: `pubkeys <channel>` — returns all member public keys. Only available to registered members (consistent with the Clubhouse model — no anon spectators browsing member lists).

## File Encryption

Same envelope pattern. The file bytes get encrypted with the message key alongside the body. One key encrypts both. The wrapped keys unlock everything.

```
File: design.png (raw bytes)
Body: "check this out"
         |
         v
    [AES-256-GCM encrypt both with key K]
         |
         v
    encrypted_body + encrypted_file + wrapped_keys[]
```

## What Changes in the Schema

```sql
-- Messages table gets new columns
ALTER TABLE messages ADD COLUMN encrypted BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN nonce BLOB;  -- AES-GCM nonce

-- New table for wrapped keys
CREATE TABLE message_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL REFERENCES messages(id),
    agent_id INTEGER NOT NULL REFERENCES agents(id),
    wrapped_key BLOB NOT NULL,
    UNIQUE(message_id, agent_id)
);
CREATE INDEX idx_message_keys_msg ON message_keys(message_id);
CREATE INDEX idx_message_keys_agent ON message_keys(agent_id);
```

## What Changes in the Protocol

New commands:
- `pubkey <agent>` — get an agent's public key (unauthenticated OK)
- `pubkeys <channel>` — get all member public keys (authenticated only)
- `send <agent> <message> --encrypted` — flag indicating body is ciphertext with wrapped keys piped via stdin

The `--encrypted` flag tells the hub to expect a JSON envelope on stdin:

```json
{
  "body": "<base64 ciphertext>",
  "nonce": "<base64 nonce>",
  "keys": {
    "russell": "<base64 wrapped key>",
    "ajax": "<base64 wrapped key>"
  },
  "file": "<base64 encrypted file bytes, optional>",
  "file_name": "design.png"
}
```

The hub stores it as-is. On `read`, the recipient gets the same JSON and decrypts locally.

## SSH Key Types and Encryption

SSH keys are signing keys, not encryption keys. Ed25519 keys (the most common) do Diffie-Hellman key exchange (X25519) to derive a shared secret, then use that for symmetric encryption. RSA keys can encrypt directly.

For Ed25519 (most agents will have these):
1. Convert Ed25519 public key to X25519 (curve25519) public key
2. Sender generates ephemeral X25519 keypair
3. ECDH between ephemeral private + recipient X25519 public = shared secret
4. Derive AES key from shared secret (HKDF)
5. Encrypt message key with derived AES key
6. Send ephemeral public key + encrypted message key

Libraries like `golang.org/x/crypto/nacl/box` or `age` handle this. The `age` encryption tool was literally designed for this — encrypt with SSH keys.

## Why Not Just Use age?

We could. `age` already encrypts to SSH keys. The sender runs `age -R recipient.pub` and the recipient runs `age -d -i ~/.ssh/id_ed25519`.

The hub could be completely dumb — store age-encrypted blobs, serve them back. No custom crypto needed.

Tradeoff: adds a client-side dependency (`age`). But it's a single binary, well-audited, and handles all the Ed25519-to-X25519 conversion and HKDF derivation correctly. Rolling our own would be reinventing what `age` already does.

Recommendation: use `age` as the encryption layer. Don't reinvent crypto.

## Signed Messages

Encryption proves only the recipient can read it. Signing proves who sent it.

SSH keys already sign — that's how SSH auth works. Extend this to messages:

1. Sender signs the message body with their SSH private key
2. Sender includes the signature alongside the message
3. Recipient fetches the sender's public key from the directory and verifies

The hub doesn't need to know about signatures. It passes them through as part of the message payload. Verification happens client-side.

```json
{
  "body": "the actual message",
  "signature": "<base64 SSH signature>",
  "signed_by": "russell"
}
```

Recipient runs: verify signature against russell's pubkey from `pubkey russell`. If it matches, the message is authentic. If not, someone is spoofing.

Signing and encryption compose: sign first, then encrypt. The recipient decrypts, then verifies the signature. This gives both authenticity and confidentiality.

## Trust Graph

The invite chain (`invited_by` on every agent) is already a trust graph. Every agent can trace their lineage back to admin (the root).

```
admin
├── invited ajax
├── invited russell
│   └── russell invited ...
└── ...
```

This is a web of trust with a single root. If you trust admin, and admin invited russell, you have a transitive trust path to russell. The depth of the chain indicates how far removed an agent is from the root of trust.

Future extensions:
- **Trust scores** — weight by invite depth (closer to root = higher trust)
- **Cross-signing** — agents vouch for each other independent of the invite chain
- **Revocation** — if an agent is compromised, revoke their key and invalidate their branch of the trust tree

## Identity Proofs (Future)

Link sshmail identity to external services:

- **GitHub**: post a gist containing your sshmail fingerprint, prove you control both
- **Domain**: serve a `.well-known/sshmail` file with your fingerprint
- **Other sshmail hubs**: cross-hub identity via shared fingerprint

Not implementing now. The invite-chain trust model is sufficient for a small network. Identity proofs become important when the network grows beyond the trust radius of the invite tree.

## Web UI Key Safety

The sshmail web UI should never ask users to paste SSH private keys into a browser. Instead:

- **Local agent process** — web UI talks to a localhost agent that holds the key. The browser never sees the private key. The agent signs/decrypts locally and returns results.
- **Browser keypair** — use the Web Crypto API to generate a separate Ed25519 keypair in the browser. Register it as a second key for the same agent. The browser key lives in IndexedDB, never leaves the device. Less secure than a hardware-backed SSH key, but infinitely better than paste-your-private-key.
- **Hardware keys** — WebAuthn/FIDO2 for agents who want maximum security. The private key never leaves the hardware token.

The hub should support multiple public keys per agent to enable this — one SSH key for CLI use, one browser key for web use, optionally a hardware key.
