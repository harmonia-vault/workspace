#!/usr/bin/env python3
"""检查拟公开源码；只报告文件/行号，不回显潜在秘密。须配合人工审查。"""
from pathlib import Path
import re
import subprocess
import sys

root = Path(__file__).resolve().parents[1]
patterns = {
    "私钥": re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----"),
    "GitHub 凭据": re.compile(r"(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,})"),
    "云访问密钥": re.compile(r"AKIA[0-9A-Z]{16}"),
    "API 凭据": re.compile(r"sk-(?:proj-)?[A-Za-z0-9_-]{24,}"),
    "本机个人路径": re.compile(r"/(?:Users|home)/[A-Za-z0-9_.-]+/"),
}
findings = []
for repo in [root, *(root / name for name in ("protocol", "core-go", "mobile", "server"))]:
    result = subprocess.run(["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=repo, check=True, capture_output=True)
    for name in set(result.stdout.decode().split("\0")) - {""}:
        path = repo / name
        if not path.is_file():
            continue
        rel = str(path.relative_to(root))
        if path.name in {".env", "auth.json", "credentials.json"} or (path.suffix in {".pem", ".key", ".sqlite", ".apk", ".exe"}):
            findings.append((rel, 0, "不应公开的文件类型"))
            continue
        try:
            content = path.read_text()
        except UnicodeDecodeError:
            continue
        for line_no, line in enumerate(content.splitlines(), 1):
            for label, pattern in patterns.items():
                matches = list(pattern.finditer(line))
                if label == "本机个人路径":
                    matches = [m for m in matches if m.group(0) not in {"/home/harmonia/", "/home/example/", "/Users/example/"}]
                if matches:
                    findings.append((rel, line_no, label))
for rel, line, label in findings:
    print(f"{rel}:{line}: {label}")
if findings:
    print(f"检查失败：{len(findings)} 项待人工确认；未打印潜在秘密。")
    sys.exit(1)
print("源码基础检查通过；还须人工审查脱敏数据和公开范围。")
