# Official negative-RSSI scan fixture

`official-negative-rssi.json` contains only synthetic Wi-Fi scan responses. It
includes complete start/status/result payloads, two test-only SSIDs, synthetic
BSSIDs, and RSSI values -42 and INT32_MIN. No device traffic, real SSIDs,
credentials or recordings were used.

Generated on the cloud server with `grpc_tools.protoc` and Python protobuf
7.36.2 from the candidate's official Espressif
[`network_provisioning` 1.3.2](https://components.espressif.com/components/espressif/network_provisioning/versions/1.3.2)
sources. `WiFiScanResult.rssi` is `int32`; negative values serialize as ten-byte
sign-extended varints. The fixture records SHA-256 for the exact three source
files, including `constants.proto` from `components/protocomm/proto`.

To regenerate on the server, set `PROTO` to the candidate's
`components/network_provisioning/proto`, `COMM` to its
`components/protocomm/proto`, and `PYTHON` to the existing protobuf environment.
Use an empty temporary directory for `GENERATED` and run from the miniprogram
directory. This needs no new JavaScript dependency.

```sh
"$PYTHON" -m grpc_tools.protoc -I"$PROTO" -I"$COMM" \
  --python_out="$GENERATED" \
  "$PROTO/network_scan.proto" "$PROTO/network_constants.proto" \
  "$COMM/constants.proto"
PYTHONPATH="$GENERATED" "$PYTHON" - <<'PY'
import os, json, hashlib, pathlib, google.protobuf
import network_scan_pb2 as scan
proto, comm = pathlib.Path(os.environ['PROTO']), pathlib.Path(os.environ['COMM'])
sources = {
    'network_provisioning/proto/network_scan.proto': proto / 'network_scan.proto',
    'network_provisioning/proto/network_constants.proto': proto / 'network_constants.proto',
    'protocomm/proto/constants.proto': comm / 'constants.proto',
}
start = scan.NetworkScanPayload(msg=scan.TypeRespScanWifiStart)
start.resp_scan_wifi_start.SetInParent()
status = scan.NetworkScanPayload(msg=scan.TypeRespScanWifiStatus)
status.resp_scan_wifi_status.scan_finished = True
status.resp_scan_wifi_status.result_count = 2
result = scan.NetworkScanPayload(msg=scan.TypeRespScanWifiResult)
for ssid, rssi in [(b'fixture-negative-rssi', -42), (b'test-only-int32-min', -2147483648)]:
    result.resp_scan_wifi_result.entries.add(
        ssid=ssid, channel=6, rssi=rssi, bssid=b'\x02\x00\x00\x00\x00\x01', auth=3)
fixture = {
    'source': 'espressif/network_provisioning 1.3.2 candidate official protobufs',
    'protobuf_version': google.protobuf.__version__,
    'source_sha256': {key: hashlib.sha256(path.read_bytes()).hexdigest() for key, path in sources.items()},
    'responses': {key: payload.SerializeToString().hex() for key, payload in [('start', start), ('status', status), ('result', result)]},
    'expected_ssids': ['fixture-negative-rssi', 'test-only-int32-min'],
    'expected_rssi': [-42, -2147483648],
}
pathlib.Path('tests/wifi-vectors/official-negative-rssi.json').write_text(json.dumps(fixture, indent=2) + '\n')
PY
```

The regression test feeds these independently generated responses to the real
`WifiProvisioner.scan()`. Before the fix, the negative RSSI produces the exact
`设备响应无效` exception while parsing an entry; after the fix, both SSIDs return.
Handwritten malformed-byte cases separately check precision, overflow,
truncation and uint32 tag/length/status-count bounds. This fixture does not
establish a successful phone/device provisioning session.
