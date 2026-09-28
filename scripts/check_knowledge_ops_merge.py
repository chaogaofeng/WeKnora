#!/usr/bin/env python3
"""Preview committed knowledge-ops integration without changing the index or checkout.

Exit 1 means text conflicts or migration version collisions remain (including
pre-existing baseline conflicts). Remote refs are not fetched by this script.
"""
import argparse
import json
import re
import subprocess


def git(*args):
    return subprocess.check_output(["git", *args], text=True).strip()


def preview(source, target):
    result = subprocess.run(
        ["git", "merge-tree", "--write-tree", "--name-only", source, target],
        capture_output=True, text=True,
    )
    if result.returncode not in (0, 1):
        raise RuntimeError(result.stderr)
    lines = result.stdout.split("\n\n", 1)[0].splitlines()
    return lines[0], lines[1:]


def migration_collisions(tree):
    versions = {}
    paths = git("ls-tree", "-r", "--name-only", tree, "migrations/").splitlines()
    for path in paths:
        match = re.fullmatch(r"migrations/(versioned|sqlite)/(\d+)_(.+)\.(up|down)\.sql", path)
        if match:
            backend, version, name, _ = match.groups()
            versions.setdefault((backend, int(version)), set()).add(name)
    return [
        {"backend": backend, "version": version, "names": sorted(names)}
        for (backend, version), names in sorted(versions.items()) if len(names) > 1
    ]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", default="upstream/main")
    parser.add_argument("--source", default="HEAD")
    parser.add_argument("--baseline", default="80f7fa8c")
    args = parser.parse_args()
    source, target, baseline = (git("rev-parse", f"{ref}^{{commit}}") for ref in (args.source, args.target, args.baseline))
    tree, current = preview(source, target)
    baseline_tree, inherited = preview(baseline, target)
    migrations = migration_collisions(tree)
    baseline_migrations = migration_collisions(baseline_tree)
    print(json.dumps({
        "source": source, "target": target, "baseline": baseline,
        "uncommitted_changes_excluded": bool(git("status", "--porcelain")),
        "conflict_files": current,
        "baseline_conflict_files": inherited,
        "additional_conflict_files": sorted(set(current) - set(inherited)),
        "migration_collisions": migrations,
        "baseline_migration_collisions": baseline_migrations,
        "additional_migration_collisions": [item for item in migrations if item not in baseline_migrations],
        "text_and_migrations_clean": not current and not migrations,
    }, ensure_ascii=False, indent=2))
    return int(bool(current or migrations))


if __name__ == "__main__":
    raise SystemExit(main())
