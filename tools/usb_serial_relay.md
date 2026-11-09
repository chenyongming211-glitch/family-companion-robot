# Mac USB 客户端与云端刷写

`usb_serial_relay.py` 只把已明确识别的原生 USB 串口经 RFC2217 转发，不保存或打印串口载荷，不执行 esptool。使用已有 pySerial，依赖安装、运行测试、esptool 和所有私有产物处理均在云端。不要把备份、NVS、设备码或固件下载到 Mac。

1. 先被动核对 USB VID/PID、序列号、设备端口及云端身份，检查串口无其他占用；不自动选择设备。
2. Mac 用已有 Python/pySerial 启动：`python tools/usb_serial_relay.py --serial /dev/cu.usbmodemXXXXX --port 42217`。监听固定为 `127.0.0.1`，不支持公网监听。
3. 复用已认证 SSH 连接建立反向转发：`ssh -S CONTROL_SOCKET -O forward -R 127.0.0.1:42217:127.0.0.1:42217 SERVER`。云端端口同样只能回环访问。
4. 云端 esptool 连接 `rfc2217://127.0.0.1:42217`。S3 原生 USB 首次进入下载模式用 `--before usb-reset`；后续确认处于 ROM/stub 后使用 `--before no-reset --after no-reset`。每次均核对报告的芯片和 MAC。
5. 云端先核对安全配置与容量，再读取两份完整 Flash 到私有 0700 目录、0600 文件；两份大小与逐字节比较均通过才准备写入。
6. 写入前核对来源、完整镜像和私有 NVS，写后完整读回验证，再正常启动。按该设备迁移计划保留云端认证和 Flash 回退资料。
7. 完成或中止后关闭 Mac relay，并用 SSH `-O cancel -R 127.0.0.1:42217:127.0.0.1:42217` 移除转发。

客户端开连接仍会按 pySerial 行为设置初始控制线及清缓冲区，`no-reset` 不代表完全无控制线变化。relay 本身不在客户端断开时额外修改 DTR/RTS。原生 USB 复位若导致断开，relay 明确退出；重新被动识别目标、重开后独立核对，禁止自动找端口或自动重试刷写。

原生 USB-Serial/JTAG 还可能在默认 RTS hard-reset 后仍保持下载模式，不能仅凭命令成功认定应用已启动。本次微雪经受控同步再次确认 stub 仍在运行后，使用官方 S3 `watchdog-reset` 触发完整系统复位，重新采样启动引脚；不刷固件或写 eFuse。WDT启用之后的最后锁定命令可能因USB复位断连而报错，必须重新识别并观察应用，不能自动重试复位或以报错代替启动结论；本次重新识别/重开后实际观察到原应用待机。用于诊断的 esptool connect 自身也可能配置易失寄存器，不称为无副作用读取。原理见 Espressif [Leaving Download Mode in USB-Serial/JTAG Mode](https://github.com/espressif/esptool/blob/master/docs/en/troubleshooting.rst#leaving-download-mode-in-usb-serialjtag-mode)。

Socket 超时 10 秒，同样约束发送；持续背压或 USB 中断会报失败，不继续安装。若备份读取未完整结束，不能把部分数据当有效备份。工具不写 eFuse。

云端验证：`python -m unittest discover -s tools -p test_usb_serial_relay.py -v`。覆盖全字节/IAC 分片双向传输、波特率与控制线、断开线程回收、受限 TCP 缓冲区下 1 秒背压、USB 故障终止及回环监听。原生硬件复位和完整备份仍单独留证。
