#!/usr/bin/env python3
"""USB client: relay bytes/control over loopback RFC2217; never log payloads.

Run esptool and store all images on the cloud, through an SSH reverse tunnel.
Uses pySerial's protocol implementation (BSD-3-Clause), with no custom framing.
Unlike the general example server, this always binds loopback and keeps control
lines unchanged between clients. Use esptool --before usb-reset for native S3 USB.
"""
import argparse
import socket
import threading

import serial
from serial.rfc2217 import PortManager


def open_listener(port):
    if not 0 <= port <= 65535:
        raise ValueError("invalid TCP port")
    listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        listener.bind(("127.0.0.1", port))
        listener.listen(1)
        return listener
    except BaseException:
        listener.close()
        raise


def relay(device, connection):
    stopped = threading.Event()
    write_lock = threading.Lock()
    errors = []

    class Connection:
        def write(self, data):
            with write_lock:
                connection.sendall(data)

    transport = Connection()
    manager = PortManager(device, transport)
    # This also bounds sendall: allow SSH backpressure during full Flash reads.
    connection.settimeout(10)

    def read_device():
        try:
            while not stopped.is_set():
                data = device.read(min(device.in_waiting or 1, 16384))
                if data:
                    transport.write(b"".join(manager.escape(data)))
        except OSError:
            errors.append("USB transport interrupted")
            stopped.set()
            try:
                connection.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass

    reader = threading.Thread(target=read_device, daemon=True)
    reader.start()
    try:
        while not stopped.is_set():
            try:
                data = connection.recv(16384)
            except socket.timeout:
                continue
            if not data:
                break
            payload = b"".join(manager.filter(data))
            if payload:
                device.write(payload)
    finally:
        stopped.set()
        reader.join(timeout=2)
    if reader.is_alive() or errors:
        raise OSError("USB transport interrupted")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=42217)
    parser.add_argument("--serial", required=True)
    args = parser.parse_args()
    if not args.serial.startswith("/dev/cu.usbmodem"):
        parser.error("use an explicitly identified native USB serial port")
    with open_listener(args.port) as listener:
        device = serial.Serial(port=None, baudrate=115200, timeout=.2,
                               write_timeout=5, exclusive=True)
        device.dtr = False
        device.rts = False
        device.port = args.serial
        try:
            device.open()
            print("USB relay listening on 127.0.0.1:%s" % args.port, flush=True)
            while True:
                connection, _ = listener.accept()
                with connection:
                    connection.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
                    relay(device, connection)
        finally:
            device.close()


if __name__ == "__main__":
    try:
        main()
    except (OSError, KeyboardInterrupt):
        raise SystemExit("USB relay stopped; no payload saved")
