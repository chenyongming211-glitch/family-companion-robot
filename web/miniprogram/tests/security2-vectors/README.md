# Public official Security 2 reference

`official.json` contains only deterministic synthetic fixtures. Never substitute real QR codes, factory keys or Wi-Fi passwords.

Source: ESP-IDF [`fff9895c82d744c7237be8847347bdd1b07c6643`](https://github.com/espressif/esp-idf/tree/fff9895c82d744c7237be8847347bdd1b07c6643).

Fetch `examples/protocols/esp_local_ctrl/scripts/security/{security.py,security2.py,srp6a.py}`, `scripts/utils/convenience.py` as `utils.py`, and `components/protocomm/proto/{constants,sec0,sec1,sec2,session}.proto` into cloud-only `official/` and `official/protos/`. The generator verifies every source against `firmware/onboarding.lock.json` before import. Install exact `tests/security2-python-requirements.txt` in an isolated cloud venv, then:

```sh
python tests/security2-official-vectors.py --official /cloud/official \
  --lock ../../firmware/onboarding.lock.json --output /cloud/regenerated.json
cmp tests/security2-vectors/official.json /cloud/regenerated.json
node --test tests/security2.test.ts
```

The harness executes unmodified official Python `Security2.setup0_request/setup0_response/setup1_request/setup1_response/encrypt_data/decrypt_data`. The challenge counterpart uses official SRP helpers and independent Python AESGCM, checking both directions and tampering. The TypeScript client must reproduce the official request/proof/ciphertext exactly and accept the official replies. This is not physical firmware or phone interoperation evidence.

Protocol details confirmed from official Python, device `esp_srp.c` and `security2.c`:

- SessionData fields `sec_ver=2` (field2) and sec2 (field12); Sec2Payload msg field1, command0 field20, response0 field21, command1 field22, response1 field23.
- SRP3072 group/generator5, SHA512, 32-byte random secret with highest bit set; client public key is384bytes as required by device C. Reject B=0 or outside group and u=0. Device C hashes the transmitted full-width A in proofs; B is variable-width. Official Python normalizes integer hash inputs; factory salt/verifier tooling matches that algorithm and emits full-width salt16/verifier384. Do not replace with an arbitrary SRP library's padding conventions.
- Device proof must match before enabling data encryption; first32bytes of SRP session key feed AES256-GCM. No AAD. Ciphertext includes16byte authentication tag.
- Only patch1: nonce is8randombytes plus4-byte BE counter initially1. The same counter increases for each request encryption and response decryption. Failed authentication, replay/counter mismatch, malformed handshake or destroyed async handshake permanently closes the client session. An exchange transport must serialize complete request/reply pairs.
- Use outer `proto-ver` to require sec_ver2/sec_patch_ver1 before establishment; that advertisement is advisory, cryptographic proof remains authoritative.

BLE framing is a separate adapter. Ordinary official GATT writes are whole protocomm messages, so repeated arbitrary20byte writes are invalid. The repository's pinned bounded frame adapter reassembles exact official bytes; see root-owned `firmware/overlays/onboarding/protocomm_ble_framing.h` and mini-program transport tests. No alternate cryptography or plaintext fallback is involved.
