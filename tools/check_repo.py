"""Check the staged Git snapshot before publishing; standard library only.

The checker reads Git's index and blob objects rather than trusting working-tree
state, so workspace cleanup cannot hide staged credentials, oversized files,
invalid JSON/UTF-8, or local-only boundary violations.
"""

import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import posixpath
import re
import subprocess
import sys
from urllib.parse import unquote, urlsplit


MAX_BYTES = 10 * 1024 * 1024
# A registered binary may opt in with "allowOversize": true (e.g. a CJK font
# that cannot be split); the hard ceiling still bounds clone size.
OVERSIZE_MAX_BYTES = 20 * 1024 * 1024
FORBIDDEN_FILE_NAMES = {"agents.md", "claude.md"}
FORBIDDEN_DIRECTORY_NAMES = {".agents", ".claude", ".local", "output", "_temp"}
LOCAL_PATHS = {
    "docs/building.md",
    "docs/performance.md",
    ".github/workflows/build.yml",
}
LOCAL_PREFIXES = ("docs/images/", "docs/performance/")
EXCEPTIONS = "references/repository-exceptions.json"

CREDENTIAL = re.compile(
    r"(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{50,}"
    r"|AKIA[0-9A-Z]{16}|sk-(?:proj-)?[A-Za-z0-9_-]{32,}"
    r"|-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----)"
)
MACHINE_PATH = re.compile(
    r"(?:[A-Z]:[\\/](?:Users[\\/][^\\/\s\"']+|_[^\\/\s\"']+)"
    r"|/(?:home|Users)/(?!(?:user|username|example)(?=[/\"'\s]|$))[^/\s\"']+)",
    re.I,
)
MD_LINK = re.compile(r"!?\[[^\]\n]*\]\(([^)\n]+)\)")
HTML_ATTR = re.compile(r"""(?:src|href)=["']([^"']+)["']""")
RESERVED = re.compile(r"^(?:con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)", re.I)
REQUIRE_LINE = re.compile(r"^\s*([^\s/]+(?:/[^\s]+)+)\s+(v[^\s]+)(\s+//\s*indirect)?\s*$")


def git(*args, data=None):
    """Run a Git command and return raw stdout bytes."""
    result = subprocess.run(["git", *args], input=data, capture_output=True)
    if result.returncode:
        raise RuntimeError("Git command failed: " + " ".join(args[:2]))
    return result.stdout


def ignored_by_project(paths, tracked_paths):
    """Evaluate tracked .gitignore rules in one batch without global excludes."""
    if not paths:
        return set()
    result = subprocess.run(
        [
            "git",
            "-c",
            "core.excludesFile=",
            "check-ignore",
            "--no-index",
            "--verbose",
            "-z",
            "--stdin",
        ],
        input=b"".join(path.encode("utf-8") + b"\0" for path in paths),
        capture_output=True,
    )
    if result.returncode not in (0, 1):
        raise RuntimeError("Git ignore check failed")
    ignored = set()
    fields = result.stdout.split(b"\0")
    for idx in range(0, len(fields) - 3, 4):
        source = fields[idx].decode("utf-8")
        pattern = fields[idx + 2]
        matched_path = fields[idx + 3].decode("utf-8")
        if source in tracked_paths and not pattern.startswith(b"!"):
            ignored.add(matched_path)
    return ignored


def staged_entries():
    entries = []
    for record in git("ls-files", "--stage", "-z").split(b"\0"):
        if not record:
            continue
        metadata, raw_path = record.split(b"\t", 1)
        mode, blob, stage = metadata.decode("ascii").split()
        entries.append((raw_path.decode("utf-8"), mode, blob, stage))
    return entries


def staged_blobs(entries):
    """Read staged blob sizes and contents in two batched cat-file calls."""
    objects = list(
        dict.fromkeys(
            blob for _, mode, blob, stage in entries if mode in ("100644", "100755") and stage == "0"
        )
    )
    if not objects:
        return {}, {}
    sizes = {}
    check_input = "".join(blob + "\n" for blob in objects).encode("ascii")
    for line in git("cat-file", "--batch-check", data=check_input).splitlines():
        blob, kind, size = line.decode("ascii").split()
        if kind != "blob":
            raise RuntimeError("Index entry is not a blob")
        sizes[blob] = int(size)
    selected = [blob for blob in objects if sizes[blob] <= OVERSIZE_MAX_BYTES]
    if not selected:
        return sizes, {}
    batch_input = "".join(blob + "\n" for blob in selected).encode("ascii")
    stream = io.BytesIO(git("cat-file", "--batch", data=batch_input))
    contents = {}
    for expected in selected:
        blob, kind, size = stream.readline().decode("ascii").split()
        data = stream.read(int(size))
        if blob != expected or kind != "blob" or len(data) != sizes[blob] or stream.read(1) != b"\n":
            raise RuntimeError("Incomplete staged blob response")
        contents[blob] = data
    return sizes, contents


def forbidden_path(path):
    parts = PurePosixPath(path).parts
    folded = path.casefold()
    if folded.startswith(LOCAL_PREFIXES) or folded in LOCAL_PATHS:
        return "removed upstream local reference or workflow"
    if any(part.casefold() in FORBIDDEN_DIRECTORY_NAMES for part in parts):
        return "local-only directory"
    if PurePosixPath(path).name.casefold() in FORBIDDEN_FILE_NAMES:
        return "local-only instruction file"
    return None


def parse_exceptions(texts, errors):
    parsed = {"ignoredFiles": set(), "binaries": {}, "symlinks": {}, "generatedLinks": {}}
    if EXCEPTIONS not in texts:
        return parsed
    try:
        doc = json.loads(texts[EXCEPTIONS])
        if not isinstance(doc, dict):
            raise ValueError("root must be object")
        for item in doc.get("ignoredFiles", []):
            path = item.get("path") if isinstance(item, dict) else None
            reason = item.get("reason") if isinstance(item, dict) else None
            if not isinstance(path, str) or not path or any(c in path for c in "*?[]"):
                raise ValueError("invalid ignoredFiles path")
            if not isinstance(reason, str) or not reason.strip():
                raise ValueError("invalid ignoredFiles reason")
            parsed["ignoredFiles"].add(path)
        for item in doc.get("binaries", []):
            path = item.get("path") if isinstance(item, dict) else None
            if (
                not isinstance(path, str)
                or path in parsed["binaries"]
                or any(c in path for c in "*?[]")
                or any(
                    not isinstance(item.get(k), str) or not item[k].strip()
                    for k in ("reason", "source", "license", "sha256")
                )
                or not re.fullmatch(r"[0-9a-f]{64}", item["sha256"])
                or type(item.get("bytes")) is not int
                or item["bytes"] < 1
                or type(item.get("allowOversize", False)) is not bool
            ):
                raise ValueError("invalid binary registration")
            parsed["binaries"][path] = item
        for item in doc.get("symlinks", []):
            path = item.get("path") if isinstance(item, dict) else None
            target = item.get("target") if isinstance(item, dict) else None
            if not isinstance(path, str) or not path or not isinstance(target, str) or not target:
                raise ValueError("invalid symlink registration")
            parsed["symlinks"][path] = item
        for item in doc.get("generatedLinks", []):
            path = item.get("path") if isinstance(item, dict) else None
            target = item.get("target") if isinstance(item, dict) else None
            if not isinstance(path, str) or not path or not isinstance(target, str) or not target:
                raise ValueError("invalid generatedLinks registration")
            parsed["generatedLinks"][(path, target)] = item
    except (KeyError, TypeError, ValueError):
        errors.append(f"{EXCEPTIONS}: invalid binary registration or exception manifest")
    return parsed


def check_binaries(blobs, binary_paths, exceptions, errors):
    registrations = exceptions["binaries"]
    for path in registrations:
        if path not in blobs:
            errors.append(f"{EXCEPTIONS}: invalid binary registration for missing path {path}")
    for path in binary_paths:
        if path not in registrations:
            errors.append(f"{path}: unregistered binary or invalid UTF-8/NUL")
    for path, entry in registrations.items():
        if path not in blobs:
            continue
        data = blobs[path]
        if len(data) != entry["bytes"] or hashlib.sha256(data).hexdigest() != entry["sha256"]:
            errors.append(f"{path}: binary registration mismatch with staged bytes")


def check_go_inventory(texts, blobs, errors):
    manifest_path = "third_party/manifests/go-mod.json"
    if manifest_path not in texts:
        return
    if "go.mod" not in texts or "go.sum" not in texts:
        errors.append(f"{manifest_path}: go.mod and go.sum must be staged")
        return
    try:
        record = json.loads(texts[manifest_path])
        mod_bytes = blobs["go.mod"].replace(b"\r\n", b"\n")
        sum_bytes = blobs["go.sum"].replace(b"\r\n", b"\n")
        if (
            hashlib.sha256(mod_bytes).hexdigest() != record["goModSha256"]
            or hashlib.sha256(sum_bytes).hexdigest() != record["goSumSha256"]
        ):
            errors.append(f"{manifest_path}: go.mod/go.sum registration mismatch")
            return
        direct = {}
        indirect = {}
        for line in texts["go.mod"].splitlines():
            m = REQUIRE_LINE.match(line)
            if m:
                mod, ver, is_ind = m.groups()
                (indirect if is_ind else direct)[mod] = ver
        expected = {}
        for line in texts["go.sum"].splitlines():
            line = line.strip()
            if not line:
                continue
            mod, ver_key, digest = line.split()
            is_mod = ver_key.endswith("/go.mod")
            ver = ver_key[: -len("/go.mod")] if is_mod else ver_key
            slot = expected.setdefault((mod, ver), {})
            slot["goModHash" if is_mod else "archiveHash"] = digest
        registered = {}
        for item in record["modules"]:
            key = (item["module"], item["version"])
            registered[key] = {
                "archiveHash": item.get("archiveHash", ""),
                "goModHash": item.get("goModHash", ""),
            }
            rel = "direct" if direct.get(item["module"]) == item["version"] else (
                "indirect" if indirect.get(item["module"]) == item["version"] else "transitive"
            )
            if item.get("relation") != rel or not str(item.get("spdxLicenseId", "")).strip():
                errors.append(f"{manifest_path}: invalid module metadata for {item.get('module')}")
        if expected != registered:
            errors.append(f"{manifest_path}: Go module inventory mismatch")
    except (KeyError, TypeError, ValueError):
        errors.append(f"{manifest_path}: invalid Go module registration")


def check_source_records(texts, blobs, errors):
    manifest_path = "third_party/manifests/upstreams.json"
    if manifest_path in texts:
        try:
            manifest = json.loads(texts[manifest_path])
            for source in manifest["sources"]:
                if not re.fullmatch(r"[0-9a-f]{40}", source["revision"]):
                    raise ValueError("invalid source revision")
                for record in source.get("trackedEvidence", []):
                    data = blobs[record["path"]].replace(b"\r\n", b"\n")
                    if len(data) != record["bytes"] or hashlib.sha256(data).hexdigest() != record["sha256"]:
                        errors.append(f"{manifest_path}: tracked source evidence mismatch")
                archive = source.get("gitArchive")
                if archive:
                    data = git(
                        "-c", "core.autocrlf=false", "-c", "core.eol=lf", "-c", "tar.umask=0002",
                        "archive", "--format=tar", source["revision"],
                    )
                    if len(data) != archive["bytes"] or hashlib.sha256(data).hexdigest() != archive["sha256"]:
                        errors.append(f"{manifest_path}: upstream archive mismatch")
        except (KeyError, TypeError, ValueError):
            errors.append(f"{manifest_path}: invalid source registration")
    check_go_inventory(texts, blobs, errors)


def markdown_prose(text):
    lines = []
    fence = None
    for line in text.splitlines():
        marker = re.match(r"^\s{0,3}(`{3,}|~{3,})", line)
        if marker:
            if fence is None:
                fence = marker[1]
            elif marker[1][0] == fence[0] and len(marker[1]) >= len(fence):
                fence = None
            continue
        if fence is None:
            lines.append(re.sub(r"(`+).*?\1", "", line))
    return "\n".join(lines)


def check_markdown_links(texts, targets, exceptions, errors):
    generated = exceptions["generatedLinks"]
    for path, text in texts.items():
        if not path.endswith(".md"):
            continue
        prose = markdown_prose(text)
        destinations = [
            raw[1 : raw.index(">")].strip() if raw.startswith("<") and ">" in raw else raw.split()[0]
            for raw in MD_LINK.findall(prose)
        ]
        destinations.extend(HTML_ATTR.findall(prose))
        for destination in destinations:
            url = urlsplit(destination)
            if url.scheme or url.netloc or not url.path:
                continue
            decoded = unquote(url.path)
            resolved = posixpath.normpath(posixpath.join(posixpath.dirname(path), decoded))
            if (path, destination) in generated or (path, resolved) in generated:
                continue
            if decoded.startswith("/") or resolved == ".." or resolved.startswith("../") or resolved not in targets:
                errors.append(f"{path}: missing tracked link: {destination}")


def check_ignore_policy(root, paths, texts, errors):
    ignore_files_match = True
    relevant_ignores = {".gitignore"}
    for path in paths:
        relevant_ignores.update(str(parent / ".gitignore") for parent in PurePosixPath(path).parents)
    for path in relevant_ignores - paths:
        if (root / path).is_file():
            errors.append(f"{path}: untracked .gitignore affects staged paths")
            ignore_files_match = False
    for path in paths:
        if PurePosixPath(path).name != ".gitignore":
            continue
        staged = texts.get(path)
        worktree = root / path
        if staged is None or not worktree.is_file() or worktree.read_text(encoding="utf-8") != staged:
            errors.append(f"{path} differs from staged .gitignore")
            ignore_files_match = False

    rules = [
        line.strip()
        for line in texts.get(".gitignore", "").splitlines()
        if line.strip() and not line.lstrip().startswith("#")
    ]
    if not rules or rules[-1] != "**/_temp/":
        errors.append("ignore boundary: the last active root rule must be **/_temp/")
    if ignore_files_match:
        probes = (
            "docs/images/probe.png",
            "docs/performance/probe.json",
            "docs/performance.md",
            "docs/building.md",
            ".github/workflows/build.yml",
            "AGENTS.md",
            "nested/CLAUDE.md",
            ".agents/probe.txt",
            "nested/.agents/probe.txt",
            "nested/.claude/probe.txt",
            "dist/probe.exe",
            "output/probe.txt",
            "nested/output/probe.txt",
            "_temp/probe.txt",
            "references/_temp/probe.txt",
            ".local/private.txt",
            "nested/.local/private.txt",
        )
        ignored = ignored_by_project(probes, paths)
        for probe in probes:
            if probe not in ignored:
                errors.append(f"ignore boundary: {probe} is exposed")


def check():
    root = Path(git("rev-parse", "--show-toplevel").decode("utf-8").strip())
    if Path.cwd().resolve() != root.resolve():
        raise RuntimeError("Run the checker from the repository root")

    entries = staged_entries()
    errors = []
    texts = {}
    blobs = {}
    binary_paths = set()
    oversize = set()
    case_names = {}
    paths = {entry[0] for entry in entries}
    targets = set(paths)

    if not entries:
        errors.append("empty Git index: stage the intended files first")

    sizes, contents = staged_blobs(entries)

    for path, mode, blob, stage in entries:
        parts = PurePosixPath(path).parts
        for offset in range(1, len(parts) + 1):
            prefix = "/".join(parts[:offset])
            targets.add(prefix)
            previous = case_names.setdefault(prefix.casefold(), prefix)
            if previous != prefix:
                errors.append(f"{path}: case collision with {previous}")

        for part in parts:
            if (
                RESERVED.match(part)
                or part.endswith((" ", "."))
                or re.search(r'[<>:"\\|?*\x00-\x1f]', part)
            ):
                errors.append(f"{path}: Windows-incompatible path")

        boundary = forbidden_path(path)
        if boundary:
            errors.append(f"{path}: forbidden {boundary}")

        if mode == "160000":
            errors.append(f"{path}: unregistered submodule; review source and pin before enabling")
            continue
        if mode not in ("100644", "100755") or stage != "0":
            errors.append(f"{path}: unsupported mode or unresolved index stage")
            continue

        size = sizes[blob]
        if size > OVERSIZE_MAX_BYTES:
            errors.append(f"{path}: exceeds 20 MiB hard file limit")
            continue
        if size > MAX_BYTES:
            oversize.add(path)
        data = contents[blob]
        blobs[path] = data
        searchable = data.decode("utf-8", errors="ignore")
        if CREDENTIAL.search(searchable):
            errors.append(f"{path}: credential pattern detected (value withheld)")
        if MACHINE_PATH.search(searchable):
            errors.append(f"{path}: machine-private path detected (value withheld)")
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError:
            binary_paths.add(path)
            continue

        if "\x00" in text:
            binary_paths.add(path)
            continue
        if text.startswith("\ufeff"):
            errors.append(f"{path}: expected UTF-8 without BOM")
        texts[path] = text
        if path.endswith(".json"):
            try:
                json.loads(text)
            except (ValueError, RecursionError):
                errors.append(f"{path}: invalid JSON")

    exceptions = parse_exceptions(texts, errors)
    ignored = ignored_by_project([p for p in paths if p not in exceptions["ignoredFiles"]], paths)
    for path in sorted(ignored):
        errors.append(f"{path}: ignored path is tracked")

    for path in oversize:
        if not exceptions["binaries"].get(path, {}).get("allowOversize", False):
            errors.append(f"{path}: exceeds 10 MiB file limit")
    check_binaries(blobs, binary_paths, exceptions, errors)
    check_source_records(texts, blobs, errors)
    check_ignore_policy(root, paths, texts, errors)
    check_markdown_links(texts, targets, exceptions, errors)

    if errors:
        print("Repository check failed:")
        for error in sorted(set(errors)):
            print("- " + error)
        return 1
    print(f"Repository check passed: {len(paths)} staged files.")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(check())
    except (OSError, UnicodeError, RuntimeError, ValueError) as exc:
        print(f"Repository check could not complete: {exc}", file=sys.stderr)
        sys.exit(1)
