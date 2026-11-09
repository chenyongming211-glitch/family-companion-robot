# Task 1 private screen-code supply

Implemented only the bitmap supply leg; no firmware UI/controller/prepare changes.

Cloud workspace: `/home/ubuntu/onboarding-screen-20261109/supply`.
SSH: `ssh -S /tmp/family-management-preview-chen/control ubuntu@82.156.48.111`.
Cloud isolated venv: `venv`; Pillow 12.0.0, esp-idf-nvs-partition-gen 0.3.0 (locked).
Official SRP: `/home/ubuntu/onboarding-20261109/task-3/official` (hash checked by tool).
IDF: `/home/ubuntu/onboarding-20261109/task-3/runtime/esp-idf` (generator hash checked).

## RED

After uploading tests, before production changes, ran in cloud workspace:

```
venv/bin/python -m unittest discover -s tools/tests -p test_onboarding_screen_code.py
venv/bin/python -m unittest discover -s firmware/tests -p test_onboarding_factory.py
```

`red-tools.log`: 3 new screen tests errored with `generate() got an unexpected keyword argument 'output_format'` (35 executions at that point due to inherited baseline discovery).
`red-factory.log`: new screen success failed with unrecognized `--screen-code`. Initial baseline failures also exposed wrong fixture selection/version: network_provisioning SRP differs from the lock and runtime IDF venv has NVS0.1.9. Corrected cloud-only isolated environment to locked 0.3.0 and `task-3/official`; never changed lock/security checks. Original logs retained in cloud. Removed duplicate inherited tests in final suite.

## GREEN

Final cloud commands (no skips):

```
export ONBOARDING_OFFICIAL=/home/ubuntu/onboarding-20261109/task-3/official
export IDF_PATH=/home/ubuntu/onboarding-20261109/task-3/runtime/esp-idf
venv/bin/python -m unittest discover -s tools/tests -p 'test_onboarding*code.py'
venv/bin/python -m unittest discover -s firmware/tests -p test_onboarding_factory.py
```

20 tools tests (original16 + new4), 8 factory tests (original6 + new2), all PASS.
Real locked official SRP + NVS generator emitted a synthetic 0x6000 Waveshare image containing screen_code and no raw claim; no hardware flashing.
Coverage: explicit official request fields, exact FRC1 header/ID/CRC/pixels, white background/no scaling, wrong dimensions/multiframe/truncation, private output/cancellation, CLI/default compatibility, NVS target/ID/header/CRC/size/private-mode rejection.

## Review and limits

Self-reviewed scoped diff and `git diff --check`. Encoder loads Pillow only in screen mode; default image path remains dependency-free and unchanged. Shared parser enforces length9848, magic, LE dimensions280, device ID and payload CRC. NVS pins no-symlink parent/read inode, bounded regular private600 owner-only single-link source; temporary screen blob is private and removed with existing private tempdir. No credentials, private pictures, audio, NVS or generated firmware downloaded to Mac/Git. All identity and image fixtures are synthetic.

Official WeChat documentation URL attempted: https://developers.weixin.qq.com/miniprogram/dev/OpenApiDoc/qrcode-link/qr-code/getUnlimitedQRCode.html ; web fetch unavailable. Request uses documented parameter names width/auto_color/line_color/is_hyaline with 280/false/black/false; actual official request/real code/phone scan remain unverified. No AppSecret, live platform request, flash, deployment or production acceptance performed.

Files: tools/onboarding_mini_code.py, tools/onboarding_screen_code.py, tools/onboarding_screen_requirements.txt, tools/onboarding_mini_code.md, tools/tests/test_onboarding_screen_code.py, firmware/tools/provision_factory_nvs.py, firmware/tests/test_onboarding_factory.py, this report.
Commit: the Task1 scoped commit containing this report (see Git log).
