"""Exercise the real terminal gate and child shell without touching user pools."""
import os
import fcntl
import struct
import pty
import select
import re
import signal
import sys
import termios
import time

binary, mode = sys.argv[1:]
pid, terminal = pty.fork()
if pid == 0:
    os.environ["TERM"] = "dumb" if mode == "dumb" else "xterm-256color"
    fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", 24 if mode == "browse" else 28, 60 if mode == "browse" else 110, 0, 0))
    settings = termios.tcgetattr(0)
    settings[3] &= ~termios.ECHO
    termios.tcsetattr(0, termios.TCSANOW, settings)
    os.execv(binary, [binary] + (["enter"] if mode in ("browse", "open") else ["prune"] if mode in ("cancel", "confirm") else []))

transcript = b""
pending = b""
reaped = False
ansi = re.compile(rb"\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07]*(?:\x07|\x1b\\)")

def expect(text):
    global transcript, pending
    marker = text.encode()
    deadline = time.monotonic() + 20
    while marker not in ansi.sub(b"", pending):
        if time.monotonic() >= deadline:
            raise AssertionError("timed out waiting for " + repr(text))
        if select.select([terminal], [], [], 0.1)[0]:
            chunk = os.read(terminal, 65536)
            if not chunk:
                raise AssertionError("terminal closed before " + repr(text))
            transcript += chunk
            pending += chunk
            if b"\x1b]11;?" in chunk:
                color = "ffff/ffff/ffff" if os.environ.get("TREEHOUSE_PTY_LIGHT") else "1c1c/2121/1b1b"
                os.write(terminal, ("\x1b]11;rgb:" + color + "\x07").encode())
    pending = b""

def capture(label):
    directory = os.environ.get("TREEHOUSE_PTY_ARTIFACTS")
    if directory:
        os.makedirs(directory, exist_ok=True)
        with open(os.path.join(directory, mode + "-" + label + ".ansi"), "wb") as file:
            file.write(transcript)

def send(text):
    os.write(terminal, text.encode())

try:
    if mode == "home":
        expect("n new branch")
        assert b"treehouse" in transcript
        assert b"Working on" not in transcript
        send("n")
        expect("Branch name")
        send("bad name\r")
        expect("invalid literal branch")
        send("\x01\x0bfeature/terminal\r")
        expect("Type 'exit' to return.")
        send("printf 'SHELL_READY\\n'\n")
        expect("SHELL_READY")
        send("exit\n")
    elif mode == "dumb":
        expect("Choose a number: ")
        send("q\n")
    elif mode == "browse":
        expect("(current)")
        capture("trees")
        send("/does-not-exist")
        expect("No matches")
        send("\x1b")
        time.sleep(0.1)
        send("q")
    elif mode == "open":
        expect("(current)")
        send("\r")
        expect("Entered worktree 1")
        send("printf 'OPEN_READY\\n'\n")
        expect("OPEN_READY")
        send("exit\n")
    elif mode == "local-only":
        expect("zz-local-work")
        assert b"remote-only" not in transcript, "remote-only ref offered as local work"
        send("q")
    elif mode == "empty":
        expect("No available branches")
        send("t")
        expect("No trees yet")
        capture("empty")
        send("q")
    elif mode == "resume":
        expect("feature/terminal")
        capture("branches")
        send("\r")
        expect("Type 'exit' to return.")
        send("printf 'RESUME_READY\\n'\n")
        expect("RESUME_READY")
        send("exit\n")
    else:
        expect("Remove the listed trees?")
        capture("cleanup")
        send("\t\r" if mode == "confirm" else "\r")
        expect("Removed" if mode == "confirm" else "Canceled")
        send("q")
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
    if mode != "dumb" and reaped:
        assert b"\x1b[?1049h" not in transcript, "inline workspace entered the alternate screen"
        assert b"\x1b[?25h" in transcript, "cursor not restored"
        settings = termios.tcgetattr(terminal)
        assert settings[3] & termios.ICANON, "terminal left in raw mode"
    os.close(terminal)
    print(transcript.decode(errors="replace"))
