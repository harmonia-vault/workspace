# Execution plan

1. **Inventory and baseline (M0)**: inspect relevant local rules/memory summaries only, verify GitHub identity/org admin, tools/VMs; save design, threat model and execution plan; initialize MIT public repositories immediately after a privacy/secret review.
2. **First tested vertical slice (M1)**: deterministic protocol/types/vectors; mature-library AEAD/signatures/HPKE; local merge/override/restoration/expiry engine and CLI; server account-scoped persistence/sequence/idempotency/current authorization; synthetic end-to-end tests. No trusted pairing bypass in a production path.
3. **Enrollment and account lifecycle (M2)**: audited mature SPAKE2 integration, phone trust confirmation/grants, HTTPS sign-in/Argon2id, email switches/verification/reset, multi-account isolation and replay checkpoints. All controls fail closed until available.
4. **Recovery and rotation (M3)**: seed derivation/envelopes, restricted recovery, bound one-use challenges, full re-entry and atomic key/envelope rotation, reset-generation invalidation and uncertain-result queries.
5. **Local system integration (M4)**: per-user boot services, atomic shell fragments/Windows user registry, explicit selected import, pause/revocation/logout, durable crash reconciliation. Verify in isolated VM accounts; do not modify host environment.
6. **Mobile + runtime acceptance (M5)**: one bounded Claude Opus 5.5 medium UI call after subscription/model validation; Flutter + Go boundary, Android platform protection/CRUD/approval/recovery; emulator acceptance. Keep iOS compatibility and report unrun iOS checks. Verify Docker SQLite restart and Workers D1/DO/Argon2 runtime behavior locally; no deployment.
7. **Review and handoff**: refresh submodule commits, repeat source privacy check, push tested code and evidence, report precise paths/links/results and all unfinished security gates. No production-ready claim.

Work is milestone-driven; implementation does not stop at scaffolding. These milestones may require several sustained task turns. Each accepted result is committed and documented with actual test evidence.
