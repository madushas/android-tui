#!/usr/bin/env python3
"""Fail when the PUBLIC tree contains private, device-specific or working-note material.

Private material (handoff notes, plans, device dumps, firmware extracts, logs, screenshots, bugreports) belongs in the
git-ignored .local/ directory. Reusable scripts and small, synthetic fixtures are fine in the public tree.

    python3 scripts/dev/privacy_check.py              scan the tree (what `git ls-files` would commit)
    python3 scripts/dev/privacy_check.py --self-test  prove the checks catch what they should, then exit

Standard library only. Exit status 1 when anything is found. Run it before every commit and before exporting a zip
(`make privacy-check`); CI runs it too.
"""
import ipaddress
import os
import re
import subprocess
import sys
import tempfile

SKIP_DIRS = {".git", "vendor", ".local", "bin", "dist", "node_modules"}
SELF = "scripts/dev/privacy_check.py"

# --- path rules --------------------------------------------------------------------------------------------------
BAD_PATH = re.compile(
    r"(^|/)(\.local|reports|artifacts|screenshots|device-dump-[^/]*|bugreport-[^/]*)(/|$)"
    r"|(^|/)(HANDOFF|CLAUDE)[^/]*\.md$"
    r"|\.(apk|pcap|pcapng|hprof|trace|img|tgz|tar\.gz)$",
    re.I,
)

# Fixture directories are the usual way real device data sneaks in. A new testdata/ directory fails until a human has
# checked that every file in it is synthetic, and then lists its path prefix here.
REVIEWED_FIXTURES = ()
TESTDATA = re.compile(r"(^|/)testdata/")

# --- content rules (text files only) -------------------------------------------------------------------------------
IPV4 = re.compile(r"(?<![\d.])(?:\d{1,3}\.){3}\d{1,3}(?![\d.])")
ALLOWED_NETS = [ipaddress.ip_network(n) for n in ("192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "127.0.0.0/8")]
ALLOWED_IPS = {"0.0.0.0", "255.255.255.255", "10.0.2.2", "10.255.255.255"}  # unspecified, broadcast, emulator host alias, routing trick
HOME_PATH = re.compile(r"(?<![\w}$).-])(?:/home|/Users)/([A-Za-z0-9._-]+)/|[A-Za-z]:\\Users\\([A-Za-z0-9._-]+)\\")  # not after $VAR / ${VAR}: those are relative
PLACEHOLDER_USERS = {"user", "you", "example", "name", "username", "me", "runner", "<user>", "USER"}
EMAIL = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+")
ALLOWED_MAIL = re.compile(r"@(example\.(com|org|net)|users\.noreply\.github\.com)$|^(noreply|git)@|^(click|dclick|rclick|wheelup|wheeldown)@", re.I)  # last group: pty_drive.py mouse steps, e.g. rclick@com.example.app
MAC = re.compile(r"(?<![0-9A-Fa-f:-])(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}(?![0-9A-Fa-f:-])")
IMEI = re.compile(r"(?i)(imei|meid)[^\n]{0,12}[0-9]{14,15}")
SERIAL = re.compile(r"(?i)(ro\.(boot\.)?serialno)[\]\"'\s:=\[]{1,6}[A-Za-z0-9]{8,}")
WORKNOTE = re.compile(r"Claude|Anthropic|ChatGPT|OpenAI|HANDOFF|CLAUDE-LOCAL|PROGRESS-TRACKER|developer-requests|claude-next-steps")
OWNER_NOTE = re.compile(r"(?i)\bthe owner'?s?\b|\bowner decided\b|\bper owner\b")

# raw dumps pasted into the tree: many lines of one machine-generated format
DUMPS = [
    ("getprop dump", re.compile(r"^\[[A-Za-z0-9_.\-]+\]: \["), 25),
    ("`pm list packages -f` dump", re.compile(r"^package:/[^=]+=[A-Za-z0-9_.]+"), 25),
    ("logcat output", re.compile(r"^\d\d-\d\d \d\d:\d\d:\d\d\.\d{3}\s+\d+\s+\d+ [VDIWEF] "), 20),
]


def is_text(data: bytes) -> bool:
    return b"\0" not in data[:8192]


def files(root):
    """Yield repo-relative paths: what git would commit when root is a git repo, otherwise a filtered walk."""
    try:
        out = subprocess.run(["git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
                             capture_output=True, check=True).stdout.decode()
        for p in filter(None, out.split("\0")):
            if p.split("/")[0] not in SKIP_DIRS and os.path.isfile(os.path.join(root, p)):
                yield p
        return
    except (OSError, subprocess.CalledProcessError):
        pass
    for dp, dn, fn in os.walk(root):
        dn[:] = [d for d in dn if d not in SKIP_DIRS]
        for f in fn:
            yield os.path.relpath(os.path.join(dp, f), root).replace(os.sep, "/")


def scan_text(rel, text):
    hits = []
    lines = text.split("\n")
    for n, line in enumerate(lines, 1):
        for m in IPV4.finditer(line):
            try:
                ip = ipaddress.ip_address(m.group())
            except ValueError:
                continue
            if ip.is_global or m.group() in ALLOWED_IPS or any(ip in net for net in ALLOWED_NETS):
                continue
            hits.append((n, f"non-public IP address {m.group()} (use 192.0.2.x, RFC 5737)"))
        for m in HOME_PATH.finditer(line):
            user = m.group(1) or m.group(2)
            if user not in PLACEHOLDER_USERS:
                hits.append((n, f"local user path ({m.group(0)}): use $HOME or a placeholder"))
        for m in EMAIL.finditer(line):
            if not ALLOWED_MAIL.search(m.group()):
                hits.append((n, f"e-mail address {m.group()}"))
        for m in MAC.finditer(line):
            octets = re.split(r"[:-]", m.group().lower())
            if len(set(octets)) > 1 and m.group().lower() not in ("aa:bb:cc:dd:ee:ff", "02:00:00:00:00:00"):
                hits.append((n, f"MAC address {m.group()}"))
        if IMEI.search(line):
            hits.append((n, "IMEI/MEID"))
        if SERIAL.search(line):
            hits.append((n, "device serial number"))
        if WORKNOTE.search(line):
            hits.append((n, "private working-note / AI-assistant reference (keep it under .local/)"))
        if OWNER_NOTE.search(line):
            hits.append((n, "private working-note language ('the owner')"))
    for label, rx, limit in DUMPS:
        c = sum(1 for line in lines if rx.match(line))
        if c >= limit:
            hits.append((0, f"looks like a raw {label} ({c} lines): keep real dumps in .local/reports/ and commit a small synthetic fixture instead"))
    return hits


def scan(root):
    problems, count = [], 0
    for rel in sorted(files(root)):
        count += 1
        if BAD_PATH.search(rel):
            problems.append(f"{rel}: private/device-derived path (belongs under the git-ignored .local/)")
            continue
        if TESTDATA.search(rel) and not rel.startswith(REVIEWED_FIXTURES):
            problems.append(f"{rel}: unreviewed fixture directory; real device/firmware data goes in .local/reports/, "
                            "and a synthetic fixture needs its path added to REVIEWED_FIXTURES in scripts/dev/privacy_check.py")
            continue
        try:
            data = open(os.path.join(root, rel), "rb").read()
        except OSError:
            continue
        if not is_text(data) or len(data) > 4_000_000:
            continue
        if rel in ("go.sum", "LICENSE", SELF):  # SELF holds the self-test strings that deliberately look private
            continue
        for n, why in scan_text(rel, data.decode("utf-8", "replace")):
            problems.append(f"{rel}:{n}: {why}" if n else f"{rel}: {why}")
    return count, problems


def self_test():
    cases = {  # file -> (content, expected number of problems >= 1 ; or 0 for must-be-clean)
        "ok/clean.go": ("// uses 192.0.2.5 and 10.0.2.2, see $HOME/.config and /home/user/x\nvar mac = \"AA:BB:CC:DD:EE:FF\"\n", 0),
        "ok/mouse.sh": ("steps=\"rclick@com.example.notes;dclick@Pixel_7|L;wheeldown@Actions\"\n", 0),
        "bad/ip.go": ("addr := \"192.168.1.20:5555\"\n", 1),
        "bad/home.sh": ("cp x /home/alice/Downloads\n", 1),
        "bad/mail.md": ("contact jane.doe@gmail.com\n", 1),
        "bad/mac.txt": ("wifi 3c:5a:b4:12:34:56\n", 1),
        "bad/note.md": ("see HANDOFF.md for details\n", 1),
        "bad/owner.md": ("extracted by the owner's own script\n", 1),
        "bad/getprop.txt": ("".join(f"[ro.x.k{i}]: [v]\n" for i in range(40)), 1),
        "bad/reports/x.txt": ("anything\n", 1),
        "bad/testdata/fixture.txt": ("harmless looking text\n", 1),
        "bad/HANDOFF.md": ("x\n", 1),
    }
    with tempfile.TemporaryDirectory() as d:
        for rel, (content, _) in cases.items():
            p = os.path.join(d, rel)
            os.makedirs(os.path.dirname(p), exist_ok=True)
            open(p, "w").write(content)
        failed = False
        for rel, (_, want) in cases.items():
            _, probs = scan(d)
            got = [x for x in probs if x.startswith(rel + ":")]
            if (len(got) > 0) != (want > 0):
                failed = True
                print(f"SELF-TEST FAIL {rel}: expected {'a problem' if want else 'clean'}, got {got}")
        # the .local tree and vendor must never be scanned
        os.makedirs(os.path.join(d, ".local"), exist_ok=True)
        open(os.path.join(d, ".local", "x.md"), "w").write("HANDOFF 192.168.0.9\n")
        _, probs = scan(d)
        if any(p.startswith(".local") for p in probs):
            failed = True
            print("SELF-TEST FAIL: .local/ must be ignored")
    print("privacy-check self-test:", "FAILED" if failed else "ok")
    return 1 if failed else 0


def main():
    if "--self-test" in sys.argv:
        return self_test()
    root = os.getcwd()
    count, problems = scan(root)
    if problems:
        print(f"privacy-check: {len(problems)} problem(s) in {count} files:")
        for p in problems:
            print("  " + p)
        print("Move private/device-derived material under .local/ (git-ignored) and replace values with placeholders.")
        return 1
    print(f"privacy-check: ok ({count} files scanned)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
