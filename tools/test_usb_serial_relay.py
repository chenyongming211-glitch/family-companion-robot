"""Cloud-only transport tests; no ESP32 or private bytes are used."""
import queue
import socket
import threading
import time
import unittest

import serial

from usb_serial_relay import open_listener, relay


class SerialFixture:
    def __init__(self):
        self.baudrate = 115200
        self.bytesize = 8
        self.parity = "N"
        self.stopbits = 1
        self.xonxoff = self.rtscts = self.dtr = self.rts = False
        self.cts = self.dsr = self.ri = self.cd = False
        self.incoming = queue.Queue()
        self.received = bytearray()
        self.changed = threading.Condition()

    @property
    def in_waiting(self):
        return 0

    def read(self, size):
        try:
            data = self.incoming.get(timeout=.1)
            if isinstance(data, BaseException):
                raise data
            return data
        except queue.Empty:
            return b""

    def write(self, data):
        with self.changed:
            self.received.extend(data)
            self.changed.notify_all()
        return len(data)

    def reset_input_buffer(self):
        pass

    def reset_output_buffer(self):
        pass


class RelayTest(unittest.TestCase):
    def test_slow_consumer_does_not_abort_backup(self):
        fixture = SerialFixture()
        errors = []
        with open_listener(0) as listener:
            def serve():
                try:
                    connection, _ = listener.accept()
                    with connection:
                        connection.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, 4096)
                        relay(fixture, connection)
                except BaseException as error:
                    errors.append(error)
            thread = threading.Thread(target=serve)
            thread.start()
            with socket.socket() as client:
                client.settimeout(5)
                client.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 4096)
                client.connect(listener.getsockname())
                negotiation = bytearray()
                while len(negotiation) < 12:
                    negotiation.extend(client.recv(12 - len(negotiation)))
                self.assertEqual(negotiation, bytes.fromhex("fffb01fffb03fffd00fffb2c"))
                payload = b"backup-transport" * 8192
                fixture.incoming.put(payload)
                time.sleep(1)
                received = bytearray()
                while len(received) < len(payload):
                    block = client.recv(16384)
                    if not block:
                        break
                    received.extend(block)
                self.assertEqual(received, payload)
            thread.join(timeout=3)
            self.assertFalse(thread.is_alive())
            self.assertEqual(errors, [])

    def test_usb_failure_closes_network_and_reports_failure(self):
        fixture = SerialFixture()
        errors = []
        with open_listener(0) as listener:
            def serve():
                try:
                    connection, _ = listener.accept()
                    with connection:
                        relay(fixture, connection)
                except OSError as error:
                    errors.append(error)
            thread = threading.Thread(target=serve)
            thread.start()
            with socket.create_connection(listener.getsockname(), timeout=5) as client:
                fixture.incoming.put(OSError("fixture USB removed"))
                while client.recv(1024):
                    pass
            thread.join(timeout=3)
            self.assertFalse(thread.is_alive())
            self.assertEqual(len(errors), 1)
            self.assertEqual(str(errors[0]), "USB transport interrupted")

    def test_binary_transport_control_and_disconnect(self):
        fixture = SerialFixture()
        errors = []
        with open_listener(0) as listener:
            self.assertEqual(listener.getsockname()[0], "127.0.0.1")

            def serve():
                try:
                    connection, _ = listener.accept()
                    with connection:
                        relay(fixture, connection)
                except BaseException as error:
                    errors.append(error)

            thread = threading.Thread(target=serve)
            thread.start()
            client = serial.serial_for_url(
                "rfc2217://127.0.0.1:%s" % listener.getsockname()[1],
                baudrate=460800, timeout=5)
            try:
                client.dtr = False
                client.rts = True
                payload = bytes(range(256)) * 257
                client.write(payload)
                with fixture.changed:
                    complete = fixture.changed.wait_for(
                        lambda: len(fixture.received) == len(payload), timeout=5)
                self.assertTrue(complete)
                self.assertEqual(bytes(fixture.received), payload)
                self.assertEqual(fixture.baudrate, 460800)
                self.assertFalse(fixture.dtr)
                self.assertTrue(fixture.rts)
                # Fragments cross escaped 0xff and all binary boundaries.
                for position in range(0, len(payload), 373):
                    fixture.incoming.put(payload[position:position + 373])
                self.assertEqual(client.read(len(payload)), payload)
            finally:
                client.close()
                thread.join(timeout=3)
            self.assertFalse(thread.is_alive(), "disconnect leaked serial reader")
            self.assertEqual(errors, [])
            # Disconnect must not issue an unsolicited device reset.
            self.assertFalse(fixture.dtr)
            self.assertTrue(fixture.rts)

    def test_invalid_tcp_port_rejected(self):
        for value in (-1, 65536):
            with self.subTest(value=value), self.assertRaises(ValueError):
                open_listener(value)


if __name__ == "__main__":
    unittest.main()
