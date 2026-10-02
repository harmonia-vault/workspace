# Harmonia / 和弦 — design baseline

Status: experimental implementation. This document specifies the target, not a claim that every control is implemented. See PLAN.md and STATUS.md for evidence.

## Product boundary

One person's devices share encrypted credentials and environment variables. Public self-hosted installations may host many strictly isolated email accounts. CLI and mobile accept a user-selected HTTPS endpoint. Environments are independent; each device has its own activation order, with later/higher priority sources overriding duplicate names and distinct names merging. Android Flutter + Go is the first mobile target; preserve an iOS path. Go CLI targets macOS, Linux and Windows. TypeScript server business logic is shared by Workers and Docker.

## Repository topology

`protocol` owns versioned wire schemas, deterministic signing encoding, threat model and interoperability vectors. `core-go` owns crypto, local reconciliation, sync, CLI and mobile bridge. `mobile` owns Flutter UI and platform local key protection. `server` owns account isolation, authorization, accepted write order, persistence, recovery and email adapters. `workspace` uses public HTTPS submodules and coordinates documentation and local tasks. No CI/CD or production deployment is part of initial implementation.

## Security and authority

Server authentication is not device trust. Password processing is exactly SHA256(password) at the client, transported over HTTPS; it is a replayable password-equivalent credential. Server uses its own random salt and Argon2id and must never log/cache that credential. It never derives vault encryption from the password. No MD5, OPAQUE or Passkey.

Each environment gets a fresh random 256-bit key. XChaCha20-Poly1305 encrypts data with versioned, domain-separated associated data. HPKE suite X25519/HKDF-SHA256/ChaCha20-Poly1305 wraps keys. Independent Ed25519 keys sign device mutations; deterministic versioned encoding binds account ID/generation, device, environment/key version, grant generation, operation and idempotency key. Mature libraries and cross-language vectors are required. Public-key trust comes from trusted management-device authorization, never an untrusted server-provided key alone. Clients keep the highest accepted checkpoint and generation to reject basic replay; external witness infrastructure is outside scope.

RO can read all keys in an authorized environment and, by possessing the symmetric key, can manufacture ciphertext. Therefore server and clients must validate signed write/delete operations and current management-signed grants. RW may explicitly edit through the app/CLI only. Admin grants and revokes roles. Every request is checked against current roles, generation, revocation and expiry, including every message after a WebSocket handshake. Server acceptance order determines LWW; identical idempotent retries retain the original sequence, and a reused ID with changed content is rejected.

New devices generate separate receiving/signing keypairs locally. A short-lived one-use pairing ceremony uses a mature SPAKE2 implementation; the short secret code never goes to the server. Challenges bind account, purpose, session and exact public keys. A trusted phone approves environments, RO/RW/Admin and expiry. Several phones may manage an account. Permanent authorization lasts until revocation. Local expiry is enforced offline and clears affected cached material; received revocations execute even while paused. Previously disclosed plaintext cannot be recalled.

Phone keys require the OS device passcode or strong biometric policy, without requiring a face or Passkey. Software keys protected by the platform must not be described as always hardware-backed. Desktop service secrets cannot require a login-unlocked Keychain: pre-login service availability trades off against disk/root access. Use per-user isolated, service-readable encrypted state with a machine protector and documented full-disk-encryption dependency; do not invent a claim of protection against a compromised root or unlocked disk.

## Local reconciliation

Shared mutation requires connectivity. Submit to the server only; after accepted persistence, update local state through the same signed sequence-pull flow, without optimistic authority changes. Offline reads use locally cached authorized values until local expiry. Overrides are explicit CLI actions, scoped by environment/name, never uploaded and usable offline. Apply overrides within an environment before priority merge. Cloud deletion or lost authorization prevents overrides from applying.

Capture the original value/existence at first takeover. Managed values overwrite same-name local values, add missing names and preserve unrelated names. External edits of managed values are corrected while active. Removal/deactivation recomputes other active sources; when none remain, restore the recorded original or remove the tool-created item. Never restore an entire shell file or registry snapshot. Closing CLI leaves service active. Pause keeps configuration and halts ordinary sync/correction; received revocations still reconcile. Normal shutdown/crash does not clear everything; restart converges idempotently. Logout/uninstall removes managed config by key with restoration.

POSIX support is sh/bash/zsh interactive local terminals and Linux SSH. Shell hooks consume an atomic managed fragment on prompt/session refresh; existing process environments cannot be changed externally. Windows applies current user's registry environment with notification for future processes. No first-release GUI/container/cron/other-service adaptation. Machine services: macOS LaunchDaemon, Linux systemd and Windows Service, least privilege and local-user isolation. Disk unlock before startup remains required. Installation and privilege/ACL tests must use VMs or isolated accounts, not the host user's real shell or registry.

## Server persistence and notification

Workers: D1 holds only email-to-account routing; each account's SQLite Durable Object owns authoritative credentials, devices/grants, ciphertext, recovery state, sessions and monotonic sequence in one atomic domain. Never duplicate security authority into D1. Docker: Node plus local SQLite on one persistent volume and a single instance, no Cloudflare dependency. Both call the same business mechanism through minimal runtime adapters. WebSocket broadcasts sequence hints only; durable incremental pull and reconnect catch-up are mandatory. Account generations invalidate stale sessions, devices and requests following destructive reset.

## Account and recovery flows

Email is the account identifier. `allowRegistration` and `requireEmailVerification` are independent switches; no invitation mode. SMTP or Cloudflare Email Service may deliver mail, with no inbound-email verification. Email-confirmed reset requires explicit destructive confirmation, erases old account data and reinitializes a new generation; it cannot recover the old vault. Old devices/sessions must fail atomically.

Recovery is a random offline seed, with purpose-separated derived encryption/signing keys. New/rotated environment keys always have recovery envelopes. Recovery proof opens a restricted session, not full management. Successful recovery must explicitly finish recovery-code rotation before authorizing devices. Rotation: generate a new seed/code, require complete re-entry, derive the new signing key from the re-entered seed, sign a server-generated single-use nonce bound to account, operation, session and recovery generation. Server atomically verifies and installs the new recovery key plus every required envelope; only then is the old code invalid. A trusted admin or restricted recovery session may initiate. Challenges are short-lived and single-use; do not rely on timestamps alone to prevent replay. On ambiguous network outcome, query status before retry/new rotation. Possession proof cannot prove that the user safely backed up the seed. Lost devices plus lost seed means old vault is unrecoverable. Recovery is not data backup; historical codes with historical ciphertext cannot be revoked retroactively.

## Email and cryptographic resource constraints

Docker SMTP: Nodemailer over implicit TLS 465 or required TLS 587, no plaintext downgrade, no secret debug. Workers opportunistic STARTTLS implementations and AUTH debug logging are unacceptable; use a verified safe implementation or Cloudflare Email Service. Arbitrary destinations may require paid Email Service; free verified-destination limitations must be documented. Deployment SMTP credentials are secrets, never repository values. Argon2id must be measured on Workers with intended parameters; resource failure is a blocker, not permission to weaken parameters.

## Verification strategy

Use synthetic fixtures for crypto tampering/binding/version tests, permission/revocation/expiry, idempotency/order, account isolation/reset, replay/recovery atomicity, offline reconciliation/original-value restoration and crash recovery. Cross-compile CLI for three OSes; run actual service behavior in available VMs before claiming support. Run TypeScript business/persistence tests against local SQLite and Workers runtime separately. Flutter analyze/build and emulator acceptance require a working official SDK; UI unit tests are excluded. Publish passed, failed and not-run results distinctly. SPAKE2/library selection, native key protection, boot-service privilege behavior and production resource measurement remain explicit gates.
