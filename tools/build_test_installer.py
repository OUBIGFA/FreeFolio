"""Build the fixed local Windows test installer from this working tree."""

import hashlib
import json
import msvcrt
import os
from pathlib import Path
import shutil
import subprocess
from datetime import datetime, timezone


ROOT = Path(__file__).resolve().parents[1]
BUILD = ROOT / "dist" / "test-installer"
RELEASE = ROOT / "output" / "release"


def output(*args):
    return subprocess.check_output(args, cwd=ROOT).decode("utf-8").strip()


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def source_tree():
    # Git's non-ignored working set includes new source files before commit.
    paths = subprocess.check_output(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], cwd=ROOT
    )
    tree = hashlib.sha256()
    for name in sorted(set(paths.split(b"\0")) - {b""}):
        path = ROOT / os.fsdecode(name)
        if path.is_file():
            tree.update(name + b"\0" + bytes.fromhex(digest(path)))
    return tree.hexdigest()


def build():
    version = output("go", "run", "./tools/ci", "version")
    module = output("go", "list", "-m", "-f", "{{.Version}}", "github.com/egoist/mygo")
    config = json.loads((ROOT / "mygo.json").read_text(encoding="utf-8"))
    head = output("git", "rev-parse", "HEAD")
    source = source_tree()
    subprocess.run(
        ["go", "run", "github.com/egoist/mygo/cmd/mygo@" + module, "build",
         "-platform", "windows/amd64", "-o", str(BUILD)], cwd=ROOT, check=True,
        env={**os.environ, "CGO_ENABLED": "0"},
    )
    bundle = BUILD / "windows-amd64"
    package = bundle / f"{config['slug']}-setup-{version}-amd64.exe"
    if head != output("git", "rev-parse", "HEAD") or source != source_tree():
        raise RuntimeError("Source changed during packaging. Run the build again.")
    name = f"FreeMD-{version}-Setup.exe"
    sha = digest(package)
    now = datetime.now(timezone.utc)
    info = {
        "version": version, "buildNumber": now.strftime("%Y%m%d%H%M%S%f"),
        "platform": "windows/amd64", "baseCommit": head, "sourceTreeSha256": source,
        "workingTreeDirty": bool(output("git", "status", "--porcelain")),
        "go": output("go", "version"), "mygo": module, "installer": name,
        "sha256": sha, "bytes": package.stat().st_size,
        "appSha256": digest(bundle / "FreeMD.exe"),
    }
    RELEASE.mkdir(parents=True, exist_ok=True)
    staged = BUILD / name
    shutil.copyfile(package, staged)
    if digest(staged) != sha:
        raise RuntimeError("Installer copy failed checksum verification.")
    checksum = BUILD / (name + ".sha256")
    checksum.write_text(f"{sha}  {name}\n", encoding="ascii")
    record = BUILD / "build-info.json"
    record.write_text(json.dumps(info, indent=2) + "\n", encoding="utf-8")
    for artifact in (staged, checksum, record):
        artifact.replace(RELEASE / artifact.name)
    print(RELEASE / name)
    print(sha)


if __name__ == "__main__":
    BUILD.mkdir(parents=True, exist_ok=True)
    # Serialize writers of the fixed artifact names, including the MyGo bundle.
    with (BUILD / "build.lock").open("a+b") as lock:
        lock.seek(0)
        msvcrt.locking(lock.fileno(), msvcrt.LK_NBLCK, 1)
        build()
