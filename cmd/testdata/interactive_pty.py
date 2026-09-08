"""Exercise the real terminal gate and child shell without touching user pools."""
import os
import pty
import select
import signal
import sys
import termios
import time

binary, mode = sys.argv[1:]
pid, terminal = pty.fork()
if pid == 0:
    settings = termios.tcgetattr(0)
    settings[3] &= ~termios.ECHO
    termios.tcsetattr(0, termios.TCSANOW, settings)
    os.execv(binary, [binary] + ([] if mode == "home" else ["prune"]))

transcript = b""
pending = b""
reaped = False

def expect(text):
    global transcript, pending
    marker = text.encode()
    deadline = time.monotonic() + 20
    while marker not in pending:
        if time.monotonic() >= deadline:
            raise AssertionError("timed out waiting for " + repr(text))
        if select.select([terminal], [], [], 0.1)[0]:
            chunk = os.read(terminal, 65536)
            if not chunk:
                raise AssertionError("terminal closed before " + repr(text))
            transcript += chunk
            pending += chunk
    pending = pending.split(marker, 1)[1]

def send(text):
    os.write(terminal, text.encode())

try:
    if mode == "home":
        expect("Choose a number: ")
        assert b"Start a new branch" in transcript
        assert b"Working on" not in transcript
        send("2\n")
        expect("Branch name (Enter to go back): ")
        send("feature/terminal\n")
        expect("Type 'exit' to return.")
        send("printf 'SHELL_READY\\n'\n")
        expect("SHELL_READY")
        send("exit\n")
        expect("Choose a number: ")
        send("q\n")
    else:
        expect("[y/N] ")
        send("y\n" if mode == "confirm" else "\n")
    deadline = time.monotonic() + 20
    while True:
        done, status = os.waitpid(pid, os.WNOHANG)
        if done:
            reaped = True
            assert os.waitstatus_to_exitcode(status) == 0, status
            break
        if time.monotonic() >= deadline:
            raise AssertionError("CLI failed to exit")
        if select.select([terminal], [], [], 0.05)[0]:
            try:
                transcript += os.read(terminal, 65536)
            except OSError:
                pass
finally:
    try:
        if not reaped:
            os.kill(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    os.close(terminal)
    print(transcript.decode(errors="replace"))
