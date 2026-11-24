"""Record Go module dependencies so the repository check detects lock changes.

Run from the repository root after changing go.mod or go.sum. The generated
manifest captures normalized SHA-256 digests of go.mod and go.sum together with
every pinned module version, checksum, and verified SPDX license identifier.
"""

import hashlib
import json
from pathlib import Path
import re


KNOWN_LICENSES = {
    "github.com/coreos/go-oidc/v3": "Apache-2.0",
    "github.com/go-jose/go-jose/v4": "Apache-2.0",
    "github.com/ebitengine/purego": "Apache-2.0",
    "github.com/egoist/mygo": "MIT",
    "github.com/gabriel-vasile/mimetype": "MIT",
    "github.com/go-text/typesetting": "Unlicense OR BSD-3-Clause",
    "github.com/go-text/typesetting-utils": "Unlicense OR BSD-3-Clause",
    "github.com/clipperhouse/uax29/v2": "MIT",
    "github.com/google/jsonschema-go": "MIT",
    "github.com/hhrutter/tiff": "BSD-3-Clause",
    "github.com/mattn/go-runewidth": "MIT",
    "github.com/pdfcpu/pdfcpu": "Apache-2.0",
    "github.com/richardlehane/mscfb": "Apache-2.0",
    "github.com/richardlehane/msoleps": "Apache-2.0",
    "github.com/tetratelabs/wazero": "Apache-2.0",
    "github.com/xdg-go/stringprep": "Apache-2.0",
    "github.com/yuin/goldmark": "MIT",
    # Files ported from libyaml are MIT; the rest is Apache-2.0.
    "go.yaml.in/yaml/v3": "MIT AND Apache-2.0",
    "golang.org/x/crypto": "BSD-3-Clause",
    "golang.org/x/image": "BSD-3-Clause",
    "golang.org/x/mod": "BSD-3-Clause",
    "golang.org/x/net": "BSD-3-Clause",
    "golang.org/x/oauth2": "BSD-3-Clause",
    "golang.org/x/sync": "BSD-3-Clause",
    "golang.org/x/sys": "BSD-3-Clause",
    "golang.org/x/term": "BSD-3-Clause",
    "golang.org/x/text": "BSD-3-Clause",
    "golang.org/x/tools": "BSD-3-Clause",
    "golang.org/x/xerrors": "BSD-3-Clause",
}

REQUIRE_LINE = re.compile(r"^\s*([^\s/]+(?:/[^\s]+)+)\s+(v[^\s]+)(\s+//\s*indirect)?\s*$")


def parse_go_mod(text):
    direct = {}
    indirect = {}
    for line in text.splitlines():
        match = REQUIRE_LINE.match(line)
        if not match:
            continue
        module, version, is_indirect = match.groups()
        if is_indirect:
            indirect[module] = version
        else:
            direct[module] = version
    return direct, indirect


def parse_go_sum(text):
    sums = {}
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        module, version_key, digest = line.split()
        is_mod = version_key.endswith("/go.mod")
        version = version_key[: -len("/go.mod")] if is_mod else version_key
        entry = sums.setdefault((module, version), {})
        entry["goModHash" if is_mod else "archiveHash"] = digest
    return sums


def build_inventory(root=Path(".")):
    go_mod_bytes = (root / "go.mod").read_bytes().replace(b"\r\n", b"\n")
    go_sum_bytes = (root / "go.sum").read_bytes().replace(b"\r\n", b"\n")
    direct, indirect = parse_go_mod(go_mod_bytes.decode("utf-8"))
    sums = parse_go_sum(go_sum_bytes.decode("utf-8"))

    target = root / "third_party/manifests/go-mod.json"
    existing_licenses = {}
    if target.is_file():
        try:
            old = json.loads(target.read_text(encoding="utf-8"))
            for pkg in old.get("modules", []):
                if isinstance(pkg, dict) and pkg.get("module") and pkg.get("spdxLicenseId"):
                    existing_licenses[pkg["module"]] = pkg["spdxLicenseId"]
        except (ValueError, TypeError):
            pass

    modules = []
    for (module, version), hashes in sorted(sums.items()):
        if module in direct and direct[module] == version:
            relation = "direct"
        elif module in indirect and indirect[module] == version:
            relation = "indirect"
        else:
            relation = "transitive"
        spdx = KNOWN_LICENSES.get(module) or existing_licenses.get(module) or "PENDING_REVIEW"
        modules.append(
            {
                "module": module,
                "version": version,
                "relation": relation,
                "sourceUrl": f"https://{module}",
                "archiveHash": hashes.get("archiveHash", ""),
                "goModHash": hashes.get("goModHash", ""),
                "spdxLicenseId": spdx,
                "consumer": "FreeFolio Go build (go.mod)",
            }
        )

    return {
        "goModSha256": hashlib.sha256(go_mod_bytes).hexdigest(),
        "goSumSha256": hashlib.sha256(go_sum_bytes).hexdigest(),
        "modules": modules,
    }


def main():
    record = build_inventory()
    target = Path("third_party/manifests/go-mod.json")
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8", newline="\n")
    print(f"Recorded {len(record['modules'])} Go modules in {target.as_posix()}")


if __name__ == "__main__":
    main()
