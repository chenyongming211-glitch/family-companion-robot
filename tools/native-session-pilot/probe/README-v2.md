# Generated-audio repair probe v2

Cloud-only test assets for Task 1 of the [repair plan](../../../docs/superpowers/plans/2026-11-14-native-playback-context-repair.md). This version is a distinct probe with three remaining primary slots. No product entry, device, microphone, service, route, firmware, or production configuration is changed.

Cloud package: `/home/ubuntu/waveshare-native-integration-20261114/source-repair-v1/.superpowers/generated-repair-probe-v2/`. The source asset `generated_audio_repair_v2.go.txt` maps to `main.go`; the test asset maps to `main_test.go`. The hidden package is excluded from default `./...` discovery. Runtime tests and execution belong on cloud.

| Candidate | SHA256 |
| --- | --- |
| main.go | `42ad92266fef3bda4c065e8990655041233943ba409889ef2cea9e528bffff46` |
| main_test.go | `ccbf416556d10bbc6d191dd188323dbaa6ff048ad788bbbad39498f16d663bae` |

## Consumed v1 evidence remains immutable

The parent's v1 run generated a completed assistant audio response with 90,240 delivered PCM24k samples and received the exact generated-item delete ACK. Its negative retrieve returned `invalid_value`, whose raw message was not retained, so lookup absence was unknown. The old connection closed. This result is not retried or refunded.

Private v1 records remain at `ops/repair-v1/protocol-v1/`. Its cloud source remains at `.superpowers/generated-repair-probe/`, with main SHA `0ef1f0d70cbc60b62e60199228ee61762d3c19e10cba25fe7a400ac6333a4146` and test SHA `9a405129a924161e3ae471849844244f0195f9d910ddc939bc4880633f0dfba1`. v5 verification checked those cloud files remained unchanged.

## Exactly three new primary intents

The new fixed protocol attempt is `generated-audio-repair-v2`, with write-once output `ops/repair-v1/protocol-v2/`. It uses the same global `attempts.jsonl`, global limits of 8 protocol / 48 primary intents, and the original repair-phase limit of four primary intents across v1 and v2 together. Every unknown intent remains consumed. v2 additionally caps its own primary intents at three and accepts only these IDs:

1. `generated-audio-repair-v2/cancel-generate`: submit a new synthetic user item, receive generated audio, send one type-only cancel, and await the same saved response's cancelled/completed terminal. Delete every generated assistant item, check exact delete ACKs, insert trusted synthetic prefix plus separate system interruption state, and verify exact created position and retrieved role/text.
2. `generated-audio-repair-v2/cancel-followup`: ask for the prefix followed by a self-generated short nonce, without revealing that nonce in user text. This completed response is also the next scenario's generated response; its actual response/item/user IDs are reused.
3. `generated-audio-repair-v2/completed-followup`: delete the preceding completed response's items, verify the same replacement gates, then ask only for the prefix. No separate initial completed generation is sent. The removed nonce is absent from the new user prompt.

Each primary intent is independently flocked and fsynced before its `response.create`. The three slots cannot be renamed through flags. A busy lock, existing intent, budget exhaustion, existing output directory, or uncertain operation stops execution. A racing completed terminal is recorded separately from successful cancellation. There is no automatic retry or budget reset.

## Explicit evidence qualities

An exact `conversation.item.deleted` ACK for each generated item is mandatory. A subsequent negative retrieve is a separate optional observation. With exactly one pending retrieve, only a valid `invalid_request_error` / `invalid_value` client error whose supplied request correlation matches may continue; an absent correlation is recorded explicitly. It always records **`absent_lookup_UNKNOWN`**, even if its message mentions that exact item and says not found. It never grants absence or internal-erasure proof from this generic error.

Only bounded facts are retained: error code/type, correlation matched/absent/mismatched, exact item mentioned, and absence phrase present. Provider error text is inspected in memory and discarded. A returned deleted item, wrong/malformed correlation, unreviewed error code/type, server error, timeout, network failure, or foreign control event stops as unknown. The subsequent replacement ACK/identity/text/order gates remain mandatory.

`protocol_acknowledged` means these required API gates succeeded; optional lookup absence and provider internal memory can still be unknown. The final language answer is a separate observation: prefix-only behavior does not prove cache/history erasure. If normalized nonce and retained prefix contain either other, both behavioral grades remain `unknown`; the retained text cannot identify leakage unambiguously. With an independent nonce, observed removed nonce is recorded explicitly. Other unrecognized behavior stays `unknown`.

The inserted prefix is a trusted synthetic fixture, not measured hardware playback. The probe records delivered audio samples and streaming SHA256, then discards audio bytes. It records real response/item IDs and bounded synthetic transcripts. No raw human voice or voice-reference sample is involved.

## Preserved guards

The approved dedicated endpoint host is exactly `llm-2d0xd4l9jv3vezeu.cn-beijing.maas.aliyuncs.com`, path `/api-ws/v1/realtime`, over wss, without user info or fragments, with default port or 443. The protected cloud config is read only via `realtimelive.LoadConfig("/etc/family-robot/realtime-test.env")`; its key is never printed or copied. The model is rewritten to `qwen-audio-3.1-realtime-plus`.

Source-path and exact-source-SHA gates target the new v2 cloud file. Server event IDs are bounded, unique, and audited. Terminal output must exactly equal the known generated-item set. Multipart content and present nonzero/invalid content indices stop as unknown. The socket retains its single reader/writer, 150-second overall bound, 40-second generation bound, eight-second cancel-terminal bound, ten-second per-read/write bound, 1 MiB messages, 32 MiB cumulative reads, 6,000 events, eight generated items per response, 45 seconds PCM per response, and bounded transcripts. Late ASR must identify a known user item in this connection.

## Fake validation v6

Cloud RED first demonstrated the missing three-response flow and disallowed-slot accounting. Separate isolated mutation runs demonstrated that the optional lookup tests reject both the legacy generic-error stop and a false `absence_confirmed` grade. Canonical v2 source was not changed by those mutation runs.

Review v6 RED reproduced false removed-tail grades for a nonce that is a substring of the retained prefix or repeats it, and a false prefix-only grade when the nonce contains that prefix. The independent-nonce leak control remained detected. The classifier now leaves all three ambiguous cases unknown.

Cloud ordinary, race, vet, and default-disabled entry checks passed seven top-level tests and nineteen subcases. Tests cover response reuse, three primary slots, the four-primary v1+v2 phase aggregate, generic/missing-correlation lookup unknown, exact-message facts without a stronger grade, nonce ambiguity and independent leakage, and hard stops for returned deleted items, wrong correlation, server/unreviewed errors, timeout, foreign controls, wrong ACK/position, duplicate event IDs, extra terminal items, and multipart content.

Private current evidence: `ops/repair-v1/probe-fake-red-v6.json` and `probe-fake-validation-v6.json`; v5 and earlier proofs remain unchanged. Before/after ledger SHA was identical: `cb67fab133a373e65bc24f08b2d5a88dbc839663208070e14978de921c175d35`, with **4 protocol / 14 primary** intents. The v2 result directory was absent. No real provider calls were made by the implementing agent.

Cloud commands from `source-repair-v1`, using `/opt/family-robot/toolchains/go1.27.3/bin/go`:

```sh
go test ./.superpowers/generated-repair-probe-v2 -count=1 -v
go test -race ./.superpowers/generated-repair-probe-v2 -count=1
go vet ./.superpowers/generated-repair-probe-v2
```

## Consumed parent-reviewed run

After specification and quality review, the parent executed this exact candidate once. It exited zero with `protocol_acknowledged`: the cancel scenario reached the same response's `cancelled` terminal; both scenarios received exact generated-item delete ACKs and replacement position ACKs with retrieved role/text. The final completed-scenario followup contained only the retained synthetic prefix. Both optional deleted-item lookups remained `absent_lookup_UNKNOWN`; this result does not establish provider internal erasure or physical-device alignment.

The shared ledger now contains **5 protocol / 17 primary** intents, SHA `b6ea198cd5517ecc9244e7bcc4f4f2b21742c3c2af3cc4d8e3dca73fed7437ad`. The phase consumed its four-primary allowance across v1/v2. Private results are at `ops/repair-v1/protocol-v2/`; launch/exit records are `protocol-parent-launch-v3.json` and `protocol-parent-exit-v3.json`. The following is the consumed command for reproducibility, **not an instruction to rerun**:

```sh
NATIVE_GENERATED_AUDIO_REPAIR_ALLOW=approved-generated-audio-repair-v2 \
NATIVE_GENERATED_AUDIO_REPAIR_SOURCE_SHA256=42ad92266fef3bda4c065e8990655041233943ba409889ef2cea9e528bffff46 \
/opt/family-robot/toolchains/go1.27.3/bin/go run ./.superpowers/generated-repair-probe-v2
```

Retain all results, including unknown outcomes. Never rerun v1, remove a result directory to retry, or refund/rename consumed intents.
