#!/usr/bin/env python3
"""Replace inline onclick attributes with data-action attributes.

Why: a Content Security Policy that disallows inline script blocks every inline
handler. Nothing happens when the control is pressed, and there is no error the
user can see — which is how "the tabs show nothing" happens while the markup and
the script both look correct.

The replacement is mechanical: onclick="name(args)" becomes
data-action="name" plus data-arg-* attributes, and a delegated listener in the
page script dispatches on the action name. Arguments are carried as data
attributes so no value is ever interpolated into code.

Run from the plugin directory. Idempotent: already-converted tags are skipped.
"""
import re
import sys

# onclick="expr" with no embedded double quote.
PATTERN = re.compile(r'onclick="([^"]+)"')

# name(arg1, arg2, ...) — the argument text is split on top-level commas.
CALL = re.compile(r'^([A-Za-z_][A-Za-z0-9_]*)\((.*)\)$', re.S)

# Handlers whose single argument is a literal string ('auto', 'cn', ...) are
# emitted as data-action-arg; the rest are emitted as-is when they are literals.
LITERAL = re.compile(r"^'(.*)'$")


def convert(match: re.Match[str], path: str, line_no: int, report: list[str]) -> str:
    expr = match.group(1).strip()
    call = CALL.match(expr)
    if not call:
        report.append(f"{path}:{line_no}: 无法解析 onclick={expr!r}（跳过）")
        return match.group(0)

    name, arg_text = call.group(1), call.group(2).strip()
    args = split_top_level(arg_text) if arg_text else []

    parts = [f'data-call="{name}"']
    for index, arg in enumerate(args):
        arg = arg.strip()
        literal = LITERAL.match(arg)
        if literal:
            parts.append(f'data-arg{index}="{literal.group(1)}"')
        elif arg == "this":
            # "this" has no data-attribute equivalent; the delegated listener
            # passes the element it matched, which is the same thing here.
            continue
        else:
            # A computed argument (a Go expression) still has to be interpolated,
            # but only its value, never as code.
            parts.append(f'data-arg{index}="` + {arg} + `"')
    return " ".join(parts)


def split_top_level(text: str) -> list[str]:
    """Split on commas that are not inside quotes or brackets."""
    out, depth, quote, current = [], 0, "", []
    for char in text:
        if quote:
            current.append(char)
            if char == quote:
                quote = ""
            continue
        if char in "'\"":
            quote = char
            current.append(char)
            continue
        if char in "([{":
            depth += 1
        elif char in ")]}":
            depth -= 1
        if char == "," and depth == 0:
            out.append("".join(current))
            current = []
            continue
        current.append(char)
    if current:
        out.append("".join(current))
    return out


def main() -> int:
    files = sys.argv[1:] or [
        "main_page.go", "checkin_page.go", "quota_page.go", "growth_page.go",
    ]
    report: list[str] = []
    total = 0

    for path in files:
        try:
            source = open(path, encoding="utf-8").read()
        except FileNotFoundError:
            continue
        lines = source.split("\n")
        out_lines = []
        for line_no, line in enumerate(lines, 1):
            if "onclick=" not in line:
                out_lines.append(line)
                continue
            replaced = PATTERN.sub(lambda m: convert(m, path, line_no, report), line)
            total += 1 if replaced != line else 0
            out_lines.append(replaced)
        open(path, "w", encoding="utf-8").write("\n".join(out_lines))

    print(f"  处理 {total} 行")
    for note in report:
        print(f"  ⚠️ {note}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
