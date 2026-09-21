import json
import os
from pathlib import Path
import sys
import time
import uuid

# The first three arguments belong to the wrapper, including an empty argument
# and shell-looking text that must remain literal.
args = sys.argv[4:]
record = {
    "executable": sys.argv[0],
    "args": sys.argv[1:],
    "cwd": os.getcwd(),
    "member": os.getenv("CSQUAD_MEMBER_ID", ""),
    "generation": os.getenv("CSQUAD_GENERATION", ""),
    "account": os.getenv("CUSTOM_ACCOUNT", ""),
    "token": os.getenv("ANTHROPIC_AUTH_TOKEN", ""),
    "model_env": os.getenv("TEST_MODEL", ""),
    "path": os.getenv("PATH", ""),
    "codex_home": os.getenv("CODEX_HOME", ""),
    "claude_home": os.getenv("CLAUDE_CONFIG_DIR", ""),
}
directory = Path(os.environ["ENGINE_CAPTURE"])
(directory / (str(uuid.uuid4()) + ".json")).write_text(json.dumps(record))
if args[:1] == ["app-server"]:
    for line in sys.stdin:
        request = json.loads(line)
        if request.get("id") == 1:
            print(json.dumps({"id": 1, "result": {}}), flush=True)
        elif request.get("id") == 2:
            print(json.dumps({"id": 2, "result": {"config": {"hooks": {}}}}), flush=True)
elif args[:1] == ["agents"]:
    print("[]")
elif args[:1] == ["queue"]:
    print("queued --thread")
elif args[:1] == ["--version"]:
    print("custom engine test")
elif args[:1] == ["--help"]:
    print("--append-system-prompt --settings --dangerously-bypass-hook-trust")
else:
    time.sleep(300)
