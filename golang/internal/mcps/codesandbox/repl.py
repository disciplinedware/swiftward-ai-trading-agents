"""
Python REPL server for Code Sandbox MCP.

One instance per agent container. Maintains persistent global state across calls.
State is preserved using pickle: before each execution the current globals are
serialised into the child subprocess so previously assigned variables are visible.

Handles /execute and /install via a multi-threaded HTTP server so concurrent
calls (e.g. parallel tool calls from the LLM) are served in parallel.

Timeout is enforced via subprocess.run(timeout=) which sends SIGKILL to the
child process - unlike threads, a timed-out process is truly killed.
"""

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import io, json, contextlib, threading, traceback, subprocess, sys, time, textwrap, tempfile, os, pickle

# Persistent globals shared across all execute calls for this agent container.
_globals: dict = {}
# Protect _globals from concurrent modification by parallel HTTP requests.
_globals_lock = threading.Lock()


def _picklable(d: dict) -> dict:
    """Return a copy of d keeping only picklable values (modules, lambdas, etc. are dropped)."""
    out = {}
    for k, v in d.items():
        if k.startswith("__"):
            continue
        try:
            pickle.dumps(v)
            out[k] = v
        except Exception:
            pass
    return out


def exec_with_timeout(code: str, timeout_sec: int) -> dict:
    """Execute code in a subprocess with the current persistent globals injected.

    The subprocess:
    1. Unpickles the parent's current _globals as its starting environment.
    2. Executes the user code against that environment.
    3. Pickles the resulting environment back to a temp file.

    The parent then merges any new/changed picklable values back into _globals,
    so the next call sees all variables set by previous calls.
    """
    # Snapshot current globals under the lock so they don't change mid-pickle.
    with _globals_lock:
        snapshot = _picklable(_globals)

    globals_file = None
    output_file = None
    script_file = None
    try:
        # Write current globals to a temp file for the subprocess to load.
        with tempfile.NamedTemporaryFile(mode="wb", suffix=".globals.pkl", delete=False) as gf:
            globals_file = gf.name
            pickle.dump(snapshot, gf)

        output_file = globals_file + ".out.pkl"

        # Build the wrapper script that loads globals, runs code, and saves results.
        script = textwrap.dedent(f"""\
            import io, contextlib, traceback, pickle, sys

            with open({repr(globals_file)}, "rb") as _f:
                _env = pickle.load(_f)

            _out = io.StringIO()
            _err = io.StringIO()
            _result = {{"stdout": "", "stderr": "", "exit_code": 0}}
            try:
                with contextlib.redirect_stdout(_out), contextlib.redirect_stderr(_err):
                    exec(compile({repr(code)}, "<sandbox>", "exec"), _env)
            except Exception:
                _result["stderr"] = traceback.format_exc()
                _result["exit_code"] = 1
            finally:
                _result["stdout"] = _out.getvalue()
                if _err.getvalue():
                    _result["stderr"] = _result["stderr"] + _err.getvalue()

            # Filter globals: skip builtins and non-picklable values.
            _new_globals = {{}}
            for _k, _v in _env.items():
                if _k.startswith("__"):
                    continue
                try:
                    import pickle as _pickle
                    _pickle.dumps(_v)
                    _new_globals[_k] = _v
                except Exception:
                    pass

            with open({repr(output_file)}, "wb") as _f:
                pickle.dump({{"result": _result, "globals": _new_globals}}, _f)
        """)

        with tempfile.NamedTemporaryFile(mode="w", suffix=".py", delete=False) as sf:
            script_file = sf.name
            sf.write(script)

        try:
            subprocess.run(
                [sys.executable, script_file],
                timeout=timeout_sec,
                check=False,
            )
        except subprocess.TimeoutExpired:
            return {
                "stdout": "",
                "stderr": f"TimeoutError: execution exceeded {timeout_sec}s and was killed",
                "exit_code": 124,
            }

        if not os.path.exists(output_file):
            return {"stdout": "", "stderr": "subprocess exited without producing output", "exit_code": 1}

        with open(output_file, "rb") as f:
            data = pickle.load(f)

        result = data["result"]
        # Merge new globals back into the persistent state.
        with _globals_lock:
            _globals.update(data["globals"])

        return result

    finally:
        for path in (globals_file, output_file, script_file):
            if path and os.path.exists(path):
                try:
                    os.unlink(path)
                except OSError:
                    pass


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(length))
        if self.path == "/execute":
            r = exec_with_timeout(body["code"], body.get("timeout", 30))
        elif self.path == "/install":
            pkg = body["package"]
            proc = subprocess.run(
                [sys.executable, "-m", "pip", "install", pkg],
                capture_output=True, text=True,
            )
            r = {"package": pkg, "exit_code": proc.returncode, "stderr": proc.stderr}
        else:
            r = {"error": "not found"}
        resp = json.dumps(r).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", len(resp))
        self.end_headers()
        self.wfile.write(resp)

    def log_message(self, *args):
        pass  # silence access logs


ThreadingHTTPServer(("0.0.0.0", 8099), Handler).serve_forever()
