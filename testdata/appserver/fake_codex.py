#!/usr/bin/python3
import json
import os
import shutil
import sys
from pathlib import Path


def schema_source():
    return Path(__file__).resolve().parent / "schema"


def generate_schema(arguments):
    try:
        output = Path(arguments[arguments.index("--out") + 1])
    except (ValueError, IndexError):
        return 2
    shutil.copytree(schema_source(), output, dirs_exist_ok=True)
    return 0


def respond(identifier, result=None, error=None):
    message = {"id": identifier}
    if error is not None:
        message["error"] = {"code": -32601, "message": error}
    else:
        message["result"] = result
    print(json.dumps(message, separators=(",", ":")), flush=True)


def app_server():
    codex_home = Path(os.environ["CODEX_HOME"])
    codex_home.mkdir(parents=True, exist_ok=True)
    rollout = codex_home / "release-smoke-control.jsonl"
    if not rollout.exists():
        rollout.write_text('{"type":"session_meta","payload":{}}\n', encoding="utf-8")
    request_log = codex_home / "appserver-requests.log"

    for line in sys.stdin:
        message = json.loads(line)
        method = message.get("method", "")
        with request_log.open("a", encoding="utf-8") as log:
            log.write(method + "\n")
        if "id" not in message:
            continue
        identifier = message["id"]
        parameters = message.get("params") or {}
        if method == "initialize":
            respond(identifier, {"codexHome": str(codex_home)})
        elif method == "thread/read":
            thread_id = parameters.get("threadId", "")
            respond(identifier, {"thread": {"id": thread_id, "path": str(rollout), "status": {"type": "idle"}}})
        elif method == "thread/inject_items":
            for item in parameters.get("items", []):
                with rollout.open("a", encoding="utf-8") as output:
                    output.write(json.dumps({"type": "response_item", "payload": item}, separators=(",", ":")) + "\n")
            respond(identifier, {})
        else:
            respond(identifier, error="unsupported fixture method: " + method)
    return 0


def main():
    arguments = sys.argv[1:]
    if arguments == ["--version"]:
        print("codex-cli fixture")
        return 0
    if arguments[:2] == ["app-server", "generate-json-schema"]:
        return generate_schema(arguments)
    if arguments == ["app-server", "--listen", "stdio://"]:
        return app_server()
    print("unsupported fixture invocation", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
