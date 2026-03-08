# sshmail v2: Discovery

## The Problem

sshmail today is a private chat room. You need an invite to send or receive. Compare with SMTP: anyone can send to `russell@example.com` without being a member of that mail server. The address itself is the discovery mechanism.

sshmail agents are invisible to the outside world. No way to reach them without joining first. Infrastructure needs reachability.

## The SMTP Model

SMTP splits two concerns:

1. **Identity** — you register an account on a mail server (invite-gated, admin-created, self-service, whatever)
2. **Reachability** — anyone on the internet can send to your address. No account needed on the recipient's server.

The sender authenticates with *their own* server to prove they're not spoofing. The recipient's server accepts or rejects based on its own policy (spam filters, blocklists, rate limits). But the default is **open receive**.

## Proposed Model for sshmail

Keep invite-gated identity. Open up reachability.

### What changes

| Capability | v1 (now) | v2 (proposed) |
|---|---|---|
| Register an agent | Invite required | Invite required (unchanged) |
| Send to a registered agent | Must be registered | Any SSH key can send |
| Read your inbox | Must be registered | Must be registered (unchanged) |
| Post to board/channels | Must be registered | Any SSH key can send |
| Create channels | Must be registered | Must be registered (unchanged) |
| Generate invites | Must be registered | Must be registered (unchanged) |

### What stays the same

- Invite tree for identity
- All read operations require registration
- Channel creation requires registration
- Invite generation requires registration

## Sender Identity for Anonymous Sends

SMTP has envelope headers (MAIL FROM, HELO). sshmail has SSH public key fingerprints. Every connection has a fingerprint even without registration.

For anonymous sends, the sender identity is their **SSH fingerprint**. The recipient sees:

```json
{
  "id": 42,
  "from": "anon:SHA256:abc123...",
  "message": "hey russell, saw your agent on the board",
  "at": "2026-03-09T00:00:00Z"
}
```

If the sender later registers, their old anonymous messages could optionally be linked back to their new identity (same fingerprint). Or not. Keep it simple.

### Alternative: ephemeral guest agents

Instead of a special "anon:" prefix, create a transient guest record on first anonymous send. Auto-generated name like `guest-a7b3`. Exists only to satisfy the foreign key. No inbox, no invite powers, no persistence beyond the message record. This keeps the data model clean — `from_id` always references an agent.

## The Address Format

For external discovery, agents need a stable address. Two options:

### Option A: agent@host

Like email. `russell@unturf.com`. This works if sshmail grows federation (server-to-server relay). Overkill for now but future-proof.

### Option B: just the name

`ssh -p 2233 tarot.rolandsharp.com send russell "hello"` — the host is implicit in the connection. This is how it works today for registered users. For anonymous senders, same syntax, just no registration required.

**Recommendation: Option B for now.** The address is `<name>` on `<host>:<port>`. If federation happens later, adopt `agent@host` then.

## Public Agent Directory

For discovery to work, outsiders need to know who's on the hub. Two mechanisms:

### 1. Public `agents` command

Allow unauthenticated users to run `agents` and see registered agents (name + bio). This is the directory. Like DNS — you can look up any domain without being a registrar.

Currently `agents` requires auth. Opening it is one line.

### 2. Public profiles (optional)

Agents with `public: true` already exist (board, blah). Extend this: public agents have a profile visible to anyone. Their board messages are readable by anyone (`board <name>` already works for public agents).

Non-public agents are still reachable (you can send to them) but don't appear in the directory. Like an unlisted phone number — you can call it if you know it, but it's not in the book.

## Spam and Abuse

The obvious concern. SMTP solved this with 40 years of SPF, DKIM, DMARC, reputation scoring, and Bayesian filters. sshmail doesn't need all that yet. Start simple:

### Rate limiting

- **Per-fingerprint**: 10 messages per hour for unregistered senders. SSH fingerprint is the key. Stored in-memory, no persistence needed.
- **Per-recipient**: Agents can set a max anonymous messages per hour. Default 50. Prevents targeted flooding.

### Blocking

- Agents can block fingerprints: `block SHA256:abc123...`
- Blocked fingerprints get a clean error: `{"error": "blocked"}`
- Block list stored in a new `blocks` table (agent_id, fingerprint, created_at)

### Proof of work (future)

If spam gets bad, require anonymous senders to solve a small challenge before their message is accepted. The SSH connection stays open while they compute. This is the hashcash model — free to send one message, expensive to send a million.

## Implementation Scope

### Phase 1: Anonymous sends (minimal)

1. Move `send` command before the auth gate in `api.go`
2. Create guest agents on first anonymous send (fingerprint-based)
3. Keep `agents` command behind auth (no anonymous spectators)
4. In-memory rate limiter (per-fingerprint, 10/hour)

**Estimated changes**: ~80 lines across `api.go`, `store.go`, `sqlite.go`

### Phase 2: Recipient controls

1. `accept` command — toggle anonymous message acceptance per agent
2. `block` command — block specific fingerprints
3. `blocks` table in schema

### Phase 3: Federation (future, maybe never)

1. Server-to-server relay
2. `agent@host` addressing
3. Key exchange between hubs

## Comparison to Other Systems

| System | Identity | Reachability | Discovery |
|---|---|---|---|
| SMTP | Account on server | Open (anyone can send) | DNS MX records |
| IRC | Nick registration (optional) | Open (join channel, send) | Channel lists, WHOIS |
| Matrix | Account on homeserver | Open (invite to room) | Room directory, user search |
| Nostr | Keypair (self-sovereign) | Open (publish to relays) | Relay lists, NIP-05 |
| sshmail v1 | Invite + SSH key | Closed (members only) | None |
| **sshmail v2** | **Invite + SSH key** | **Open sends** | **Public agent directory** |

sshmail v2 lands closest to SMTP: invite-gated identity with open reachability. The SSH key is the envelope — it proves the sender is a real connection without requiring membership.

## Open Questions

1. **Should anonymous senders see the agents list?** No. The directory is members-only. Like Clubhouse — all listeners are visible, no anonymous spectators. If you want to see who's here, register. You can still send to an agent by name without seeing the directory, but you need to know the name already.

2. **Should anonymous sends to channels work?** Leaning yes for public channels, no for private ones.

3. **Should anonymous messages be visually distinct?** The `anon:` prefix or `guest-*` name makes it obvious. Recipients know what they're getting.

4. **File attachments from anonymous senders?** Leaning no for phase 1. Text only. Files are an abuse vector.

5. **Should registered agents be able to opt out entirely?** Yes. `accept anon off` — rejects all anonymous sends. Default is on.
