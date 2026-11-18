#!/usr/bin/env python3
"""Keep the code base maintainable: fail when a Go file or function grows past a hard limit.

    python3 scripts/dev/size_check.py              check main.go, cli*.go and internal/ (vendor excluded)
    python3 scripts/dev/size_check.py --self-test  prove the checker catches what it should

Limits (lines): source file 600, test file 800, function 100. Files and functions above 80% of a limit are listed as
warnings so growth is visible long before it becomes a problem. Split by responsibility rather than raising a limit.
Standard library only. Exit status 1 when a hard limit is exceeded.
"""
import glob
import os
import re
import sys
import tempfile

MAX_FILE, MAX_TEST_FILE, MAX_FUNC = 600, 800, 100
WARN = 0.8
SKIP_DIRS = {"vendor", ".git", ".local", "bin", "dist"}


def go_files(root):
    for dp, dn, fn in os.walk(root):
        dn[:] = [d for d in dn if d not in SKIP_DIRS]
        for f in fn:
            if f.endswith(".go"):
                yield os.path.relpath(os.path.join(dp, f), root).replace(os.sep, "/")


def functions(lines):
    """(name, length) of every multi-line top-level function in a gofmt'd file (closing brace at column 0)."""
    out, i = [], 0
    while i < len(lines):
        m = re.match(r"^func (\([^)]*\) )?(\w+)", lines[i])
        if m and not (lines[i].count("{") == lines[i].count("}") and lines[i].rstrip().endswith("}")):
            j = i + 1
            while j < len(lines) and lines[j] != "}":
                j += 1
            out.append((m.group(2), j - i + 1))
            i = j
        i += 1
    return out


def check(root):
    problems, warnings, n = [], [], 0
    for rel in sorted(go_files(root)):
        n += 1
        lines = open(os.path.join(root, rel), encoding="utf-8", errors="replace").read().split("\n")
        total = len(lines) - (1 if lines and lines[-1] == "" else 0)
        limit = MAX_TEST_FILE if rel.endswith("_test.go") else MAX_FILE
        if total > limit:
            problems.append(f"{rel}: {total} lines (limit {limit}) — split it by responsibility")
        elif total > limit * WARN:
            warnings.append(f"{rel}: {total} lines (limit {limit})")
        for name, size in functions(lines):
            if size > MAX_FUNC:
                problems.append(f"{rel}: func {name} is {size} lines (limit {MAX_FUNC}) — extract steps into functions")
            elif size > MAX_FUNC * WARN:
                warnings.append(f"{rel}: func {name} is {size} lines (limit {MAX_FUNC})")
    return n, problems, warnings


def self_test():
    with tempfile.TemporaryDirectory() as d:
        big = "package x\n\n" + "".join(f"var v{i} = {i}\n" for i in range(650))
        fn = "package x\n\nfunc long() {\n" + "\tprintln(1)\n" * 120 + "}\n\nfunc short() {\n\tprintln(1)\n}\n\nfunc one() int { return 1 }\n"
        ok = "package x\n\nfunc fine() {\n" + "\tprintln(1)\n" * 20 + "}\n"
        for name, text in (("big.go", big), ("fn.go", fn), ("ok.go", ok)):
            open(os.path.join(d, name), "w").write(text)
        os.makedirs(os.path.join(d, "vendor"))
        open(os.path.join(d, "vendor", "huge.go"), "w").write(big)
        _, problems, _ = check(d)
        got = " | ".join(problems)
        good = ("big.go: 6" in got) and ("func long is" in got) and ("short" not in got) and ("ok.go" not in got) and ("vendor" not in got)
    print("size-check self-test:", "ok" if good else "FAILED: " + got)
    return 0 if good else 1


def main():
    if "--self-test" in sys.argv:
        return self_test()
    n, problems, warnings = check(os.getcwd())
    for w in warnings:
        print("  warn  " + w)
    if problems:
        print(f"size-check: {len(problems)} problem(s) in {n} Go files:")
        for p in problems:
            print("  FAIL  " + p)
        return 1
    print(f"size-check: ok ({n} Go files; limits: file {MAX_FILE}, test {MAX_TEST_FILE}, function {MAX_FUNC} lines)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
